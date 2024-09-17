package google

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"strconv"
	"strings"

	"elasticsearch-vm-autoscaler/internal/elasticsearch"
	"elasticsearch-vm-autoscaler/internal/globals"

	compute "cloud.google.com/go/compute/apiv1"
	computepb "cloud.google.com/go/compute/apiv1/computepb"
	"google.golang.org/api/iterator"
)

// AddNodeToMIG increases the size of the Managed Instance Group (MIG) by 1, if it has not reached the maximum limit.
func AddNodeToMIG(projectID, zone, migName string, debugMode bool) error {
	ctx := context.Background()

	// Create a new Compute client for managing the MIG
	client, err := createComputeClient(ctx, compute.NewInstanceGroupManagersRESTClient)
	if err != nil {
		return fmt.Errorf("failed to create Instance Group Managers client: %v", err)
	}
	defer client.Close()

	// Get the current target size of the MIG
	targetSize, err := getMIGTargetSize(ctx, client, projectID, zone, migName)
	if err != nil {
		return fmt.Errorf("failed to get MIG target size: %v", err)
	}

	// Get the scaling limits (minimum and maximum)
	_, maxSize, err := getMIGScalingLimits()
	if err != nil {
		return fmt.Errorf("failed to get MIG scaling limits: %v", err)
	}

	// Check if the MIG has reached its maximum size
	if targetSize >= maxSize {
		fmt.Printf("MIG has reached its maximum size (%d), no further scaling is possible.\n", maxSize)
		return nil
	}

	// Create a request to resize the MIG by increasing the target size by 1
	req := &computepb.ResizeInstanceGroupManagerRequest{
		Project:              projectID,
		Zone:                 zone,
		InstanceGroupManager: migName,
		Size:                 targetSize + 1,
	}

	// Resize the MIG if not in debug mode
	if !debugMode {
		_, err = client.Resize(ctx, req)
		return err
	}
	return nil
}

// RemoveNodeFromMIG decreases the size of the Managed Instance Group (MIG) by 1, if it has not reached the minimum limit.
func RemoveNodeFromMIG(projectID, zone, migName, elasticURL, elasticUser, elasticPassword string, debugMode bool) error {
	ctx := context.Background()

	// Create a new Compute client for managing the MIG
	client, err := createComputeClient(ctx, compute.NewInstanceGroupManagersRESTClient)
	if err != nil {
		return fmt.Errorf("failed to create Instance Group Managers client: %v", err)
	}
	defer client.Close()

	// Get the current target size of the MIG
	targetSize, err := getMIGTargetSize(ctx, client, projectID, zone, migName)
	if err != nil {
		return fmt.Errorf("failed to get MIG target size: %v", err)
	}

	// Get the scaling limits (minimum and maximum)
	minSize, _, err := getMIGScalingLimits()
	if err != nil {
		return fmt.Errorf("failed to get MIG scaling limits: %v", err)
	}

	// Check if the MIG has reached its minimum size
	if targetSize <= minSize {
		log.Printf("MIG has reached the minimum size (%d/%d), no further scaling down is possible.\n", targetSize, minSize)
		return nil
	}

	// Get a random instance from the MIG to remove
	instanceToRemove, err := GetInstanceToRemove(ctx, client, projectID, zone, migName)
	if err != nil {
		log.Printf("Error getting instance to remove: %v", err)
		return err
	}

	// If not in debug mode, drain the node from Elasticsearch before removal
	if !debugMode {
		log.Printf("Instance to remove: %s", instanceToRemove)
		err = elasticsearch.DrainElasticsearchNode(elasticURL, instanceToRemove, elasticUser, elasticPassword)
		if err != nil {
			log.Printf("Error draining Elasticsearch node: %v", err)
			return err
		}
	}

	// Create a request to delete the selected instance and reduce the MIG size
	instanceURL := fmt.Sprintf("projects/%s/zones/%s/instances/%s", projectID, zone, instanceToRemove)
	deleteReq := &computepb.DeleteInstancesInstanceGroupManagerRequest{
		Project:              projectID,
		Zone:                 zone,
		InstanceGroupManager: migName,
		InstanceGroupManagersDeleteInstancesRequestResource: &computepb.InstanceGroupManagersDeleteInstancesRequest{
			Instances: []string{instanceURL},
		},
	}

	// Delete the instance if not in debug mode
	if !debugMode {
		_, err = client.DeleteInstances(ctx, deleteReq)
		if err != nil {
			log.Fatalf("Error deleting instance: %v", err)
		}
	}

	// If not in debug mode, remove the elasticsearch node from cluster settings
	if !debugMode {
		err = elasticsearch.ClearElasticsearchClusterSettings(elasticURL, elasticUser, elasticPassword)
		if err != nil {
			log.Fatalf("Error clearing Elasticsearch cluster settings: %v", err)
			return err
		}
	}

	return nil
}

// getMIGScalingLimits retrieves the minimum and maximum scaling limits for a Managed Instance Group (MIG).
func getMIGScalingLimits() (int32, int32, error) {
	// Get min and max size from environment variables and parse to integers
	minSize, _ := strconv.ParseInt(globals.GetEnv("MIN_SIZE", "1"), 10, 32)
	maxSize, _ := strconv.ParseInt(globals.GetEnv("MAX_SIZE", "1"), 10, 32)

	return int32(minSize), int32(maxSize), nil
}

// getMIGTargetSize retrieves the current target size of a Managed Instance Group (MIG).
func getMIGTargetSize(ctx context.Context, client *compute.InstanceGroupManagersClient, projectID, zone, migName string) (int32, error) {
	// Create a request to get the MIG details
	req := &computepb.GetInstanceGroupManagerRequest{
		Project:              projectID,
		Zone:                 zone,
		InstanceGroupManager: migName,
	}

	// Get the MIG details from Google Cloud
	mig, err := client.Get(ctx, req)
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
func GetInstanceToRemove(ctx context.Context, client *compute.InstanceGroupManagersClient, projectID, zone, migName string) (string, error) {
	// Get the list of instances in the MIG
	instanceNames, err := getMIGInstanceNames(ctx, client, projectID, zone, migName)
	if err != nil {
		return "", err
	}
	if len(instanceNames) == 0 {
		return "", fmt.Errorf("no instances found in the MIG")
	}

	// Randomly select an instance to remove
	return getInstanceNameFromURL(instanceNames[rand.Intn(len(instanceNames))]), nil
}

// getMIGInstanceNames retrieves the list of instance names in a Managed Instance Group (MIG).
func getMIGInstanceNames(ctx context.Context, client *compute.InstanceGroupManagersClient, projectID, zone, migName string) ([]string, error) {
	// Create a request to list the managed instances in the MIG
	req := &computepb.ListManagedInstancesInstanceGroupManagersRequest{
		Project:              projectID,
		Zone:                 zone,
		InstanceGroupManager: migName,
	}

	// Call the API and get an iterator for the managed instances
	it := client.ListManagedInstances(ctx, req)

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
func CheckMIGMinimumSize(projectID, zone, migName string, debugMode bool) error {
	ctx := context.Background()

	// Create a Compute client for managing the MIG
	client, err := createComputeClient(ctx, compute.NewInstanceGroupManagersRESTClient)
	if err != nil {
		return fmt.Errorf("failed to create Instance Group Managers client: %v", err)
	}
	defer client.Close()

	// Get the current target size of the MIG
	targetSize, err := getMIGTargetSize(ctx, client, projectID, zone, migName)
	if err != nil {
		return fmt.Errorf("failed to get MIG target size: %v", err)
	}

	// Get the scaling limits (minimum and maximum)
	minSize, _, err := getMIGScalingLimits()
	if err != nil {
		return fmt.Errorf("failed to get MIG scaling limits: %v", err)
	}

	// If the MIG size is below the minimum, scale it up to the minimum size
	if targetSize < minSize {
		log.Printf("MIG size is below the limit (%d/%d), scaling it up...", targetSize, minSize)
		req := &computepb.ResizeInstanceGroupManagerRequest{
			Project:              projectID,
			Zone:                 zone,
			InstanceGroupManager: migName,
			Size:                 minSize,
		}

		// Resize the MIG if not in debug mode
		if !debugMode {
			_, err = client.Resize(ctx, req)
			return err
		}
	}
	return nil
}
