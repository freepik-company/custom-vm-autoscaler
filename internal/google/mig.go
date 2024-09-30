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
	_, maxSize := getMIGScalingLimits(ctx)

	// Check if the MIG has reached its maximum size
	if targetSize >= maxSize {
		return 0, 0, fmt.Errorf("MIG has reached its maximum size (%d/%d), no further scaling is possible", targetSize, maxSize)
	}

	// Create a request to resize the MIG by increasing the target size by 1
	req := &computepb.ResizeInstanceGroupManagerRequest{
		Project:              ctx.Config.Infrastructure.GCP.ProjectID,
		Zone:                 ctx.Config.Infrastructure.GCP.Zone,
		InstanceGroupManager: ctx.Config.Infrastructure.GCP.MIGName,
		Size:                 targetSize + 1,
	}

	// Resize the MIG if not in debug mode
	if !ctx.Config.Autoscaler.DebugMode {
		_, err = client.Resize(ctxConn, req)
		if err != nil {
			return 0, 0, err
		} else {
			log.Printf("Scaled up MIG successfully %d/%d", targetSize+1, maxSize)
		}
	}
	return targetSize + 1, maxSize, nil
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
	minSize, _ := getMIGScalingLimits(ctx)

	// Check if the MIG has reached its minimum size
	if targetSize <= minSize {
		return 0, 0, "", fmt.Errorf("MIG has reached its minimum size (%d/%d), no further scaling down is possible", targetSize, minSize)
	}

	// Get a random instance from the MIG to remove
	instanceToRemove, err := GetInstanceToRemove(ctxConn, client, ctx)
	if err != nil {
		return 0, 0, "", fmt.Errorf("error draining Elasticsearch node: %v", err)
	}

	// If not in debug mode, drain the node from Elasticsearch before removal
	if !ctx.Config.Autoscaler.DebugMode {
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
		} else {
			log.Printf("Scaled down MIG successfully %d/%d", targetSize-1, minSize)
		}
		// Wait 90 seconds until instance is fully deleted
		// Google Cloud has a deletion timeout of 90 seconds max
		time.Sleep(90 * time.Second)

		// Remove the elasticsearch node from cluster settings
		err = elasticsearch.ClearElasticsearchClusterSettings(ctx)
		if err != nil {
			return 0, 0, "", fmt.Errorf("error clearing Elasticsearch cluster settings: %v", err)
		}
		log.Printf("Cleared up elasticsearch settings for draining node")
	}

	return targetSize - 1, minSize, instanceToRemove, nil
}

// getMIGScalingLimits retrieves the minimum and maximum scaling limits for a Managed Instance Group (MIG).
func getMIGScalingLimits(ctx *v1alpha1.Context) (int32, int32) {
	currentTime := time.Now().UTC()
	currentWeekday := int(currentTime.Weekday())

	for _, scalingConfig := range ctx.Config.Autoscaler.AdvancedCustomScalingConfiguration {
		// Check if current day is within the critical period days
		criticalPeriodDays := strings.Split(scalingConfig.Days, ",")
		for _, criticalPeriodDay := range criticalPeriodDays {
			if strings.TrimSpace(criticalPeriodDay) == strconv.Itoa(currentWeekday) {
				if scalingConfig.HoursUTC != "" {
					criticalPeriodHours := strings.Split(scalingConfig.HoursUTC, "-")
					if len(criticalPeriodHours) != 2 {
						log.Fatalf("Invalid hours format in advanced_scaling_configuration. Expected start and end hours separated by a dash (e.g., 4:00:00-6:00:00)")
						return int32(ctx.Config.Autoscaler.MinSize), int32(ctx.Config.Autoscaler.MaxSize)
					}
					// Parse start and end hours
					startHour, err := time.Parse("15:04:05", criticalPeriodHours[0])
					if err != nil {
						log.Printf("Error parsing start hour: %v", err)
						return int32(ctx.Config.Autoscaler.MinSize), int32(ctx.Config.Autoscaler.MaxSize)
					}
					endHour, err := time.Parse("15:04:05", criticalPeriodHours[1])
					if err != nil {
						log.Printf("Error parsing end hour: %v", err)
						return int32(ctx.Config.Autoscaler.MinSize), int32(ctx.Config.Autoscaler.MaxSize)
					}

					// Adjust start and end times to match the current date
					startTime := time.Date(currentTime.Year(), currentTime.Month(), currentTime.Day(), startHour.Hour(), startHour.Minute(), startHour.Second(), 0, currentTime.Location())
					endTime := time.Date(currentTime.Year(), currentTime.Month(), currentTime.Day(), endHour.Hour(), endHour.Minute(), endHour.Second(), 0, currentTime.Location())

					// Check if current time is within the critical period
					if currentTime.After(startTime) && currentTime.Before(endTime) {
						return int32(scalingConfig.MinSize), int32(scalingConfig.MaxSize)
					}
				} else {
					// If no hours are provided, assume critical period is for the entire day
					return int32(scalingConfig.MinSize), int32(scalingConfig.MaxSize)
				}
			}
		}
	}

	return int32(ctx.Config.Autoscaler.MinSize), int32(ctx.Config.Autoscaler.MaxSize)
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
func CheckMIGMinimumSize(ctx *v1alpha1.Context) (int32, error) {
	ctxConn := context.Background()

	// Create a Compute client for managing the MIG
	client, err := createComputeClient(ctxConn, ctx, compute.NewInstanceGroupManagersRESTClient)
	if err != nil {
		return 0, fmt.Errorf("failed to create Instance Group Managers client: %v", err)
	}
	defer client.Close()

	// Get the current target size of the MIG
	targetSize, err := getMIGTargetSize(ctxConn, client, ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get MIG target size: %v", err)
	}

	// Get the scaling limits (minimum and maximum)
	minSize, _ := getMIGScalingLimits(ctx)

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
				return 0, err
			}
		}
		return minSize, nil
	} else {
		return 0, fmt.Errorf("MIG size is already at the minimum limit (%d/%d)", targetSize, minSize)
	}

}
