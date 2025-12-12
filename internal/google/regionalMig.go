package google

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"math/big"
	"strings"
	"time"

	"custom-vm-autoscaler/api/v1alpha1"
	"custom-vm-autoscaler/internal/elasticsearch"
	"custom-vm-autoscaler/internal/slack"

	compute "cloud.google.com/go/compute/apiv1"
	computepb "cloud.google.com/go/compute/apiv1/computepb"
	"google.golang.org/api/iterator"
)

// AddNodeToRegionalMIG increases the size of the Managed Instance Group (MIG) by 1, if it has not reached the maximum limit.
func AddNodeToRegionalMIG(ctx *v1alpha1.Context) (int32, int32, error) {
	ctxConn, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Create a new Compute client for managing the MIG
	client, err := createComputeClient(ctxConn, ctx, compute.NewRegionInstanceGroupManagersRESTClient)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to create Instance Group Managers client: %v", err)
	}
	defer client.Close()

	// Get the current target size of the MIG
	targetSize, err := getRegionalMIGTargetSize(ctxConn, client, ctx)
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
	req := &computepb.ResizeRegionInstanceGroupManagerRequest{
		Project:              ctx.Config.Infrastructure.GCP.ProjectID,
		Region:               ctx.Config.Infrastructure.GCP.Region,
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
func RemoveNodeFromRegionalMIG(ctx *v1alpha1.Context) (int32, int32, string, error) {
	// Step 1: Use a context with timeout for initial GCP operations (get info)
	var targetSize, minSize, desiredSize int32
	var instanceToRemove, zoneToRemove string
	var client *compute.RegionInstanceGroupManagersClient

	ctxInfo, cancelInfo := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancelInfo()

	// Create a new Compute client for managing the MIG
	client, err := createComputeClient(ctxInfo, ctx, compute.NewRegionInstanceGroupManagersRESTClient)
	if err != nil {
		return 0, 0, "", fmt.Errorf("failed to create Instance Group Managers client: %v", err)
	}
	defer client.Close()

	// Get the current target size of the MIG
	targetSize, err = getRegionalMIGTargetSize(ctxInfo, client, ctx)
	if err != nil {
		return 0, 0, "", fmt.Errorf("failed to get MIG target size: %v", err)
	}
	log.Printf("Current size of MIG is %d nodes", targetSize)

	// Get the scaling limits (minimum and maximum)
	minSize, _, _, scaleDownThreshold := getMIGScalingLimits(ctx)

	// Get the desired size of the MIG
	desiredSize = targetSize - scaleDownThreshold

	// Check if the MIG has reached its minimum size
	if desiredSize < minSize {
		log.Printf("MIG has reached its minimum size (%d/%d), no further scaling down is possible", targetSize, minSize)
		return -1, -1, "", nil
	}

	// Get a random instance from the MIG to remove
	instanceToRemove, zoneToRemove, err = GetRegionalInstanceToRemove(ctxInfo, client, ctx)
	if err != nil {
		return 0, 0, "", fmt.Errorf("error getting instance to remove: %v", err)
	}

	// Step 2: Drain Elasticsearch node (no timeout here, it has its own internal timeout)
	// Check if elasticsearch is defined in the target
	if ctx.Config.Target.Elasticsearch.URL != "" {
		// Try to drain elasticsearch node with a timeout
		log.Printf("Instance to remove: %s. Draining from elasticsearch cluster", instanceToRemove)
		err = elasticsearch.DrainElasticsearchNode(ctx, instanceToRemove)
		if err != nil {
			return 0, 0, "", fmt.Errorf("error draining Elasticsearch node: %v", err)
		}
		log.Printf("Instance drained successfully from elasticsearch cluster")
	}

	// Step 3: Use a separate context with timeout for the delete operation
	ctxDelete, cancelDelete := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancelDelete()

	// Create a request to delete the selected instance and reduce the MIG size
	instanceURL := fmt.Sprintf("projects/%s/zones/%s/instances/%s", ctx.Config.Infrastructure.GCP.ProjectID, zoneToRemove, instanceToRemove)
	deleteReq := &computepb.DeleteInstancesRegionInstanceGroupManagerRequest{
		Project:              ctx.Config.Infrastructure.GCP.ProjectID,
		Region:               ctx.Config.Infrastructure.GCP.Region,
		InstanceGroupManager: ctx.Config.Infrastructure.GCP.MIGName,
		RegionInstanceGroupManagersDeleteInstancesRequestResource: &computepb.RegionInstanceGroupManagersDeleteInstancesRequest{
			Instances: []string{instanceURL},
		},
	}

	// Delete the instance if not in debug mode
	if !ctx.Config.Autoscaler.DebugMode {
		_, err = client.DeleteInstances(ctxDelete, deleteReq)
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

		// Remove the elasticsearch node from cluster settings
		err = elasticsearch.ClearElasticsearchClusterSettings(ctx, instanceToRemove)
		if err != nil {
			return 0, 0, "", fmt.Errorf("error clearing Elasticsearch cluster settings: %v", err)
		}
		log.Printf("Cleared up elasticsearch settings for draining node")
	}

	return desiredSize, minSize, instanceToRemove, nil
}

// getMIGTargetSize retrieves the current target size of a Managed Instance Group (MIG).
func getRegionalMIGTargetSize(ctxConn context.Context, client *compute.RegionInstanceGroupManagersClient, ctx *v1alpha1.Context) (int32, error) {
	// Create a request to get the MIG details
	req := &computepb.GetRegionInstanceGroupManagerRequest{
		Project:              ctx.Config.Infrastructure.GCP.ProjectID,
		Region:               ctx.Config.Infrastructure.GCP.Region,
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

// GetInstanceToRemove retrieves a random instance from the MIG to be removed.
func GetRegionalInstanceToRemove(ctxConn context.Context, client *compute.RegionInstanceGroupManagersClient, ctx *v1alpha1.Context) (string, string, error) {
	// Get the list of instances in the MIG
	instanceNames, err := getRegionalMIGInstanceNames(ctxConn, client, ctx)
	if err != nil {
		return "", "", err
	}
	if len(instanceNames) == 0 {
		return "", "", fmt.Errorf("no instances found in the MIG")
	}

	// Randomly select an instance to remove
	randomIndex, err := rand.Int(rand.Reader, big.NewInt(int64(len(instanceNames))))
	if err != nil {
		return "", "", fmt.Errorf("error selecting random instance: %v", err)
	}
	randomInstance := int(randomIndex.Int64())

	return instanceNames[randomInstance]["name"], instanceNames[randomInstance]["zone"], nil
}

// getMIGInstanceNames retrieves the list of instance names in a Managed Instance Group (MIG).
func getRegionalMIGInstanceNames(ctxConn context.Context, client *compute.RegionInstanceGroupManagersClient, ctx *v1alpha1.Context) ([]map[string]string, error) {
	// Create a request to list the managed instances in the MIG
	req := &computepb.ListManagedInstancesRegionInstanceGroupManagersRequest{
		Project:              ctx.Config.Infrastructure.GCP.ProjectID,
		Region:               ctx.Config.Infrastructure.GCP.Region,
		InstanceGroupManager: ctx.Config.Infrastructure.GCP.MIGName,
	}

	// Call the API and get an iterator for the managed instances
	it := client.ListManagedInstances(ctxConn, req)

	// Store the instance names and zones in a slice of maps
	var instanceNames []map[string]string

	// Iterate through the instances and collect their names and zones
	for {
		instance, err := it.Next()
		if err == iterator.Done {
			break // End of iteration
		}
		if err != nil {
			return nil, fmt.Errorf("failed to list managed instances: %v", err)
		}

		// Create a map with instance name and zone
		instanceInfo := map[string]string{
			"name": getInstanceNameFromURL(*instance.Instance),
			"zone": getZoneFromURL(*instance.Instance),
		}

		// Append the instance info to the list
		instanceNames = append(instanceNames, instanceInfo)
	}

	return instanceNames, nil
}

// getZoneFromURL extracts the zone from a Google Cloud instance URL
func getZoneFromURL(instanceURL string) string {
	parts := strings.Split(instanceURL, "/")
	if len(parts) > 0 {
		return parts[len(parts)-3]
	}
	return ""
}

// CheckMIGMinimumSize ensures that the MIG has at least the minimum number of instances running.
func CheckRegionalMIGMinimumSize(ctx *v1alpha1.Context) error {
	ctxConn, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Create a Compute client for managing the MIG
	client, err := createComputeClient(ctxConn, ctx, compute.NewRegionInstanceGroupManagersRESTClient)
	if err != nil {
		return fmt.Errorf("failed to create Instance Group Managers client: %v", err)
	}
	defer client.Close()

	// Get the current target size of the MIG
	targetSize, err := getRegionalMIGTargetSize(ctxConn, client, ctx)
	if err != nil {
		return fmt.Errorf("failed to get MIG target size: %v", err)
	}

	// Get the scaling limits (minimum and maximum) and scaling up/down thresholds
	minSize, _, _, _ := getMIGScalingLimits(ctx)

	// If the MIG size is below the minimum, scale it up to the minimum size
	if targetSize < minSize {
		log.Printf("MIG size is below the limit (%d/%d), scaling it up...", targetSize, minSize)
		req := &computepb.ResizeRegionInstanceGroupManagerRequest{
			Project:              ctx.Config.Infrastructure.GCP.ProjectID,
			Region:               ctx.Config.Infrastructure.GCP.Region,
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
