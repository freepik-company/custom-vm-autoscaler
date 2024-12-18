package google

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"math/big"
	"strconv"
	"strings"
	"time"

	"custom-vm-autoscaler/api/v1alpha1"
	"custom-vm-autoscaler/internal/elasticsearch"
	"custom-vm-autoscaler/internal/slack"

	compute "cloud.google.com/go/compute/apiv1"
	computepb "cloud.google.com/go/compute/apiv1/computepb"
	"google.golang.org/api/iterator"
)

// AddNodeToMIG increases the size of the Managed Instance Group (MIG) by 1, if it has not reached the maximum limit.
func AddNodeToMIG(ctx *v1alpha1.Context) (int32, int32, error) {
	ctxConn := context.Background()

	// Create a new Compute client for managing the MIG
	client, err := createComputeClient(ctxConn, ctx, compute.NewInstanceGroupManagersRESTClient)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to create Instance Group Managers client: %v", err)
	}
	defer client.Close()

	// Get the current target size of the MIG
	targetSize, err := getMIGTargetSize(ctxConn, client, ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get MIG target size: %v", err)
	}
	log.Printf("Current size of MIG is %d nodes", targetSize)

	// Get the scaling limits (minimum and maximum)
	_, maxSize, scaleUpThreshold, _ := getMIGScalingLimits(ctx)

	// Get the desired size of the MIG
	desiredSize := targetSize + scaleUpThreshold

	// Check if the MIG has reached its maximum size
	if desiredSize > maxSize {
		log.Printf("MIG has reached its maximum size (%d/%d), no further scaling is possible", targetSize, maxSize)
		return -1, -1, nil
	}

	// Create a request to resize the MIG by increasing the target size by 1
	req := &computepb.ResizeInstanceGroupManagerRequest{
		Project:              ctx.Config.Infrastructure.GCP.ProjectID,
		Zone:                 ctx.Config.Infrastructure.GCP.Zone,
		InstanceGroupManager: ctx.Config.Infrastructure.GCP.MIGName,
		Size:                 desiredSize,
	}

	// Resize the MIG if not in debug mode
	if !ctx.Config.Autoscaler.DebugMode {
		_, err = client.Resize(ctxConn, req)
		if err != nil {
			return 0, 0, err
		} else {
			log.Printf("Scaled up MIG successfully %d/%d", desiredSize, maxSize)
		}
	}
	return desiredSize, maxSize, nil
}

// RemoveNodeFromMIG decreases the size of the Managed Instance Group (MIG) by 1, if it has not reached the minimum limit.
func RemoveNodeFromMIG(ctx *v1alpha1.Context) (int32, int32, string, error) {
	ctxConn := context.Background()

	// Create a new Compute client for managing the MIG
	client, err := createComputeClient(ctxConn, ctx, compute.NewInstanceGroupManagersRESTClient)
	if err != nil {
		return 0, 0, "", fmt.Errorf("failed to create Instance Group Managers client: %v", err)
	}
	defer client.Close()

	// Get the current target size of the MIG
	targetSize, err := getMIGTargetSize(ctxConn, client, ctx)
	if err != nil {
		return 0, 0, "", fmt.Errorf("failed to get MIG target size: %v", err)
	}
	log.Printf("Current size of MIG is %d nodes", targetSize)

	// Get the scaling limits (minimum and maximum)
	minSize, _, _, scaleDownThreshold := getMIGScalingLimits(ctx)

	// Get the desired size of the MIG
	desiredSize := targetSize - scaleDownThreshold

	// Check if the MIG has reached its minimum size
	if desiredSize < minSize {
		log.Printf("MIG has reached its minimum size (%d/%d), no further scaling down is possible", targetSize, minSize)
		return -1, -1, "", nil
	}

	// Get a random instance from the MIG to remove
	instanceToRemove, err := GetInstanceToRemove(ctxConn, client, ctx)
	if err != nil {
		return 0, 0, "", fmt.Errorf("error getting instance to remove: %v", err)
	}

	// If not in debug mode, drain the node from Elasticsearch before removal
	// Chech if elasticsearch is defined in the target
	if ctx.Config.Target.Elasticsearch.URL != "" {

		// Try to drain elasticsearch node with a timeout
		log.Printf("Instance to remove: %s. Draining from elasticsearch cluster", instanceToRemove)
		err = elasticsearch.DrainElasticsearchNode(ctx, instanceToRemove)
		if err != nil {
			return 0, 0, "", fmt.Errorf("error draining Elasticsearch node: %v", err)
		}
		log.Printf("Instance drained successfully from elasticsearch cluster")
	}

	// Create a request to delete the selected instance and reduce the MIG size
	instanceURL := fmt.Sprintf("projects/%s/zones/%s/instances/%s", ctx.Config.Infrastructure.GCP.ProjectID, ctx.Config.Infrastructure.GCP.Zone, instanceToRemove)
	deleteReq := &computepb.DeleteInstancesInstanceGroupManagerRequest{
		Project:              ctx.Config.Infrastructure.GCP.ProjectID,
		Zone:                 ctx.Config.Infrastructure.GCP.Zone,
		InstanceGroupManager: ctx.Config.Infrastructure.GCP.MIGName,
		InstanceGroupManagersDeleteInstancesRequestResource: &computepb.InstanceGroupManagersDeleteInstancesRequest{
			Instances: []string{instanceURL},
		},
	}

	// Delete the instance if not in debug mode
	if !ctx.Config.Autoscaler.DebugMode {
		_, err = client.DeleteInstances(ctxConn, deleteReq)
		if err != nil {
			return 0, 0, "", fmt.Errorf("error deleting instance: %v", err)
		}
	}

	log.Printf("Scaled down MIG successfully %d/%d", desiredSize, minSize)

	// Wait 90 seconds until instance is fully deleted
	// Google Cloud has a deletion timeout of 90 seconds max
	if !ctx.Config.Autoscaler.DebugMode {
		time.Sleep(90 * time.Second)
	} else {
		log.Printf("Debug mode enabled. Skipping 90 seconds timeout until instance deletion")
	}

	// Chech if elasticsearch is defined in the target
	if ctx.Config.Target.Elasticsearch.URL != "" {
		err = elasticsearch.ClearElasticsearchClusterSettings(ctx, instanceToRemove)
		if err != nil {
			log.Printf("Error clearing Elasticsearch cluster settings: %v", err)

			// Attempt to revert cluster settings if the timeout occurred
			revertErr := elasticsearch.UndrainElasticsearchNode(ctx, instanceToRemove)
			if revertErr != nil {
				log.Printf("Error reverting Elasticsearch cluster settings: %v", revertErr)
				return 0, 0, "", fmt.Errorf("error reverting Elasticsearch cluster settings after timeout: %v", revertErr)
			}

			log.Printf("Node %s successfully reintegrated into Elasticsearch cluster after timeout", instanceToRemove)
			return 0, 0, "", fmt.Errorf("node reintegrated due to timeout during removal")
		}
		log.Printf("Cleared Elasticsearch settings for draining node")
	}

	return desiredSize, minSize, instanceToRemove, nil
}

// getMIGScalingLimits retrieves the minimum and maximum scaling limits for a Managed Instance Group (MIG) and how many nodes to scale up/down.
func getMIGScalingLimits(ctx *v1alpha1.Context) (int32, int32, int32, int32) {
	currentTime := time.Now().UTC()
	currentWeekday := int(currentTime.Weekday())
	var scaleDownThreshold int32 = 1

	for _, scalingConfig := range ctx.Config.Autoscaler.AdvancedCustomScalingConfiguration {

		// Set default values if not provided
		if scalingConfig.ScaleUpThreshold == 0 {
			scalingConfig.ScaleUpThreshold = ctx.Config.Autoscaler.ScaleUpThreshold
		}
		if scalingConfig.MinSize == 0 {
			scalingConfig.MinSize = ctx.Config.Autoscaler.MinSize
		}
		if scalingConfig.MaxSize == 0 {
			scalingConfig.MaxSize = ctx.Config.Autoscaler.MaxSize
		}

		// Check if current day is within the critical period days
		criticalPeriodDays := strings.Split(scalingConfig.Days, ",")
		for _, criticalPeriodDay := range criticalPeriodDays {
			if strings.TrimSpace(criticalPeriodDay) == strconv.Itoa(currentWeekday) {
				if scalingConfig.HoursUTC != "" {
					criticalPeriodHours := strings.Split(scalingConfig.HoursUTC, "-")
					if len(criticalPeriodHours) != 2 {
						log.Fatalf("Invalid hours format in advanced_scaling_configuration. Expected start and end hours separated by a dash (e.g., 4:00:00-6:00:00)")
						return int32(ctx.Config.Autoscaler.MinSize), int32(ctx.Config.Autoscaler.MaxSize), int32(ctx.Config.Autoscaler.ScaleUpThreshold), scaleDownThreshold
					}
					// Parse start and end hours
					startHour, err := time.Parse("15:04:05", criticalPeriodHours[0])
					if err != nil {
						log.Printf("Error parsing start hour: %v", err)
						return int32(ctx.Config.Autoscaler.MinSize), int32(ctx.Config.Autoscaler.MaxSize), int32(ctx.Config.Autoscaler.ScaleUpThreshold), scaleDownThreshold
					}
					endHour, err := time.Parse("15:04:05", criticalPeriodHours[1])
					if err != nil {
						log.Printf("Error parsing end hour: %v", err)
						return int32(ctx.Config.Autoscaler.MinSize), int32(ctx.Config.Autoscaler.MaxSize), int32(ctx.Config.Autoscaler.ScaleUpThreshold), scaleDownThreshold
					}

					// Adjust start and end times to match the current date
					startTime := time.Date(currentTime.Year(), currentTime.Month(), currentTime.Day(), startHour.Hour(), startHour.Minute(), startHour.Second(), 0, currentTime.Location())
					endTime := time.Date(currentTime.Year(), currentTime.Month(), currentTime.Day(), endHour.Hour(), endHour.Minute(), endHour.Second(), 0, currentTime.Location())

					// Check if current time is within the critical period
					if currentTime.After(startTime) && currentTime.Before(endTime) {
						return int32(scalingConfig.MinSize), int32(scalingConfig.MaxSize), int32(scalingConfig.ScaleUpThreshold), scaleDownThreshold
					}
				} else {
					// If no hours are provided, assume critical period is for the entire day
					return int32(scalingConfig.MinSize), int32(scalingConfig.MaxSize), int32(scalingConfig.ScaleUpThreshold), scaleDownThreshold
				}
			}
		}
	}

	return int32(ctx.Config.Autoscaler.MinSize), int32(ctx.Config.Autoscaler.MaxSize), int32(ctx.Config.Autoscaler.ScaleUpThreshold), scaleDownThreshold
}

// getMIGTargetSize retrieves the current target size of a Managed Instance Group (MIG).
func getMIGTargetSize(ctxConn context.Context, client *compute.InstanceGroupManagersClient, ctx *v1alpha1.Context) (int32, error) {
	// Create a request to get the MIG details
	req := &computepb.GetInstanceGroupManagerRequest{
		Project:              ctx.Config.Infrastructure.GCP.ProjectID,
		Zone:                 ctx.Config.Infrastructure.GCP.Zone,
		InstanceGroupManager: ctx.Config.Infrastructure.GCP.MIGName,
	}

	// Get the MIG details from Google Cloud
	mig, err := client.Get(ctxConn, req)
	if err != nil {
		return 0, fmt.Errorf("failed to get MIG: %v", err)
	}

	// Return the current target size of the MIG
	return mig.GetTargetSize(), nil
}

// getInstanceNameFromURL parses the Google Cloud instance name to get just the hostname
// and not the full path
func getInstanceNameFromURL(instanceURL string) string {
	parts := strings.Split(instanceURL, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return ""
}

// GetInstanceToRemove retrieves a random instance from the MIG to be removed.
func GetInstanceToRemove(ctxConn context.Context, client *compute.InstanceGroupManagersClient, ctx *v1alpha1.Context) (string, error) {
	// Get the list of instances in the MIG
	instanceNames, err := getMIGInstanceNames(ctxConn, client, ctx)
	if err != nil {
		return "", err
	}
	if len(instanceNames) == 0 {
		return "", fmt.Errorf("no instances found in the MIG")
	}

	// Randomly select an instance to remove
	randomIndex, err := rand.Int(rand.Reader, big.NewInt(int64(len(instanceNames))))
	if err != nil {
		return "", fmt.Errorf("error selecting random instance: %v", err)
	}
	randomInstance := int(randomIndex.Int64())

	return getInstanceNameFromURL(instanceNames[randomInstance]), nil
}

// getMIGInstanceNames retrieves the list of instance names in a Managed Instance Group (MIG).
func getMIGInstanceNames(ctxConn context.Context, client *compute.InstanceGroupManagersClient, ctx *v1alpha1.Context) ([]string, error) {
	// Create a request to list the managed instances in the MIG
	req := &computepb.ListManagedInstancesInstanceGroupManagersRequest{
		Project:              ctx.Config.Infrastructure.GCP.ProjectID,
		Zone:                 ctx.Config.Infrastructure.GCP.Zone,
		InstanceGroupManager: ctx.Config.Infrastructure.GCP.MIGName,
	}

	// Call the API and get an iterator for the managed instances
	it := client.ListManagedInstances(ctxConn, req)

	// Store the instance names in a slice
	var instanceNames []string

	// Iterate through the instances and collect their names
	for {
		instance, err := it.Next()
		if err == iterator.Done {
			break // End of iteration
		}
		if err != nil {
			return nil, fmt.Errorf("failed to list managed instances: %v", err)
		}

		// Append the instance name to the list
		instanceNames = append(instanceNames, *instance.Instance)
	}

	return instanceNames, nil
}

// CheckMIGMinimumSize ensures that the MIG has at least the minimum number of instances running.
func CheckMIGMinimumSize(ctx *v1alpha1.Context) error {
	ctxConn := context.Background()

	// Create a Compute client for managing the MIG
	client, err := createComputeClient(ctxConn, ctx, compute.NewInstanceGroupManagersRESTClient)
	if err != nil {
		return fmt.Errorf("failed to create Instance Group Managers client: %v", err)
	}
	defer client.Close()

	// Get the current target size of the MIG
	targetSize, err := getMIGTargetSize(ctxConn, client, ctx)
	if err != nil {
		return fmt.Errorf("failed to get MIG target size: %v", err)
	}

	// Get the scaling limits (minimum and maximum) and scaling up/down thresholds
	minSize, _, _, _ := getMIGScalingLimits(ctx)

	// If the MIG size is below the minimum, scale it up to the minimum size
	if targetSize < minSize {
		log.Printf("MIG size is below the limit (%d/%d), scaling it up...", targetSize, minSize)
		req := &computepb.ResizeInstanceGroupManagerRequest{
			Project:              ctx.Config.Infrastructure.GCP.ProjectID,
			Zone:                 ctx.Config.Infrastructure.GCP.Zone,
			InstanceGroupManager: ctx.Config.Infrastructure.GCP.MIGName,
			Size:                 minSize,
		}

		// Resize the MIG if not in debug mode
		if !ctx.Config.Autoscaler.DebugMode {
			_, err = client.Resize(ctxConn, req)
			if err != nil {
				return err
			}
			log.Printf("MIG %s scaled up to its minimum size %d", ctx.Config.Infrastructure.GCP.MIGName, minSize)
			if ctx.Config.Notifications.Slack.WebhookURL != "" {
				message := fmt.Sprintf("MIG %s scaled up to its minimum size %d", ctx.Config.Infrastructure.GCP.MIGName, minSize)
				err = slack.NotifySlack(message, ctx.Config.Notifications.Slack.WebhookURL)
				if err != nil {
					log.Printf("Error sending Slack notification: %v", err)
				}
			}
			time.Sleep(time.Duration(ctx.Config.Autoscaler.DefaultCooldownPeriodSec) * time.Second)
		}
	}

	return nil

}
