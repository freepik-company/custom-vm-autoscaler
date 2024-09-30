package elasticsearch

import (
	"bytes"
	"crypto/tls"
	"custom-vm-autoscaler/api/v1alpha1"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"

	"github.com/elastic/go-elasticsearch/v8"
)

// DrainElasticsearchNode drains an Elasticsearch node and performs a controlled shutdown.
// elasticURL: The URL of the Elasticsearch cluster.
// nodeName: The name of the node to shut down.
// username: The username for basic authentication.
// password: The password for basic authentication.
func DrainElasticsearchNode(ctx *v1alpha1.Context, nodeName string) error {

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: ctx.Config.Service.Elasticsearch.SSLInsecureSkipVerify,
			MinVersion:         tls.VersionTLS13,
		},
	}

	// Create elasticsearch config for connection
	cfg := elasticsearch.Config{
		Addresses: []string{ctx.Config.Service.Elasticsearch.URL},
		Username:  ctx.Config.Service.Elasticsearch.User,
		Password:  ctx.Config.Service.Elasticsearch.Password,
		Transport: tr,
	}

	// Creates new client
	es, err := elasticsearch.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("failed to create Elasticsearch client: %w", err)
	}

	// Get Elasticsearch node IP using the nodeName to delete
	nodeIP, err := getNodeIP(es, nodeName)
	if err != nil {
		return fmt.Errorf("failed to get node IP: %w", err)
	}

	// Exclude the node IP from routing allocations
	err = updateClusterSettings(es, nodeIP)
	if err != nil {
		return fmt.Errorf("failed to update cluster settings: %w", err)
	}

	// Wait until the node is removed from the cluster
	err = waitForNodeRemoval(es, nodeName)
	if err != nil {
		return fmt.Errorf("failed while waiting for node removal: %w", err)
	}

	return nil
}

// getNodeIP retrieves the IP address of the Elasticsearch node.
func getNodeIP(es *elasticsearch.Client, nodeName string) (string, error) {

	// Request to get the nodes information
	res, err := es.Cat.Nodes(
		es.Cat.Nodes.WithFormat("json"),
		es.Cat.Nodes.WithV(true),
	)
	if err != nil {
		return "", fmt.Errorf("failed to get nodes information: %w", err)
	}
	defer res.Body.Close()

	// Reads the response
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return "", fmt.Errorf("error reading response body: %w", err)
	}

	// Parse response in JSON
	var nodes []v1alpha1.NodeInfo
	err = json.Unmarshal([]byte(string(body)), &nodes)
	if err != nil {
		return "", fmt.Errorf("error deserializing JSON: %w", err)
	}

	// Find the IP address for the node with the hostname
	for _, node := range nodes {
		if node.Name == nodeName {
			if node.IP != "" {
				return node.IP, nil
			}
		}
	}

	return "", fmt.Errorf("node IP not found")
}

// updateClusterSettings updates the cluster settings to exclude a specific node IP.
func updateClusterSettings(es *elasticsearch.Client, nodeIP string) error {

	// _cluster/settings to set
	settings := map[string]map[string]string{
		"persistent": {
			"cluster.routing.allocation.exclude._ip": nodeIP,
		},
	}

	// Parse settings in JSON
	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("failed to marshal settings to JSON: %w", err)
	}

	// Execute PUT _cluster/settings command
	req := bytes.NewReader(data)
	res, err := es.Cluster.PutSettings(req)
	if err != nil {
		return fmt.Errorf("failed to update cluster settings: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("error updating cluster settings: %s", res.String())
	}

	return nil
}

// waitForNodeRemoval waits for the node to be removed from the cluster.
func waitForNodeRemoval(es *elasticsearch.Client, nodeName string) error {

	// Prepare regex to match shards with
	re, err := regexp.Compile(nodeName)
	if err != nil {
		log.Fatalf("Error compilando regex: %v", err)
	}

	for {
		// Get _cat/shards to check if nodeName has any shard inside
		res, err := es.Cat.Shards(
			es.Cat.Shards.WithFormat("json"),
			es.Cat.Shards.WithV(true),
		)
		if err != nil {
			return fmt.Errorf("failed to get shards information: %w", err)
		}
		defer res.Body.Close()

		// Get response
		body, err := io.ReadAll(res.Body)
		if err != nil || string(body) == "" {
			return fmt.Errorf("error reading response body: %w", err)
		}

		// Parse response in JSON
		var shards []v1alpha1.ShardInfo
		err = json.Unmarshal([]byte(string(body)), &shards)
		if err != nil {
			return fmt.Errorf("error deserializing JSON: %w", err)
		}

		// Check if nodeName has any shards inside it
		nodeFound := false
		for _, shard := range shards {
			if re.MatchString(shard.Node) {
				nodeFound = true
			}
		}

		// If nodeFound is false, there are not any shard inside it. It is ready to delete
		if !nodeFound {
			log.Printf("node %s is fully empty and ready to delete", nodeName)
			break
		}

	}

	return nil
}

// clearClusterSettings removes the node exclusion from cluster settings.
func ClearElasticsearchClusterSettings(ctx *v1alpha1.Context) error {

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: ctx.Config.Service.Elasticsearch.SSLInsecureSkipVerify,
			MinVersion:         tls.VersionTLS13,
		},
	}

	// Configure elasticsearch connection
	cfg := elasticsearch.Config{
		Addresses: []string{ctx.Config.Service.Elasticsearch.URL},
		Username:  ctx.Config.Service.Elasticsearch.User,
		Password:  ctx.Config.Service.Elasticsearch.Password,
		Transport: tr,
	}

	// Create elastic client
	es, err := elasticsearch.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("failed to create Elasticsearch client: %w", err)
	}

	// _cluster/settings to set after the node deletion
	settings := map[string]map[string]any{
		"persistent": {
			"cluster.routing.allocation.exclude._ip": nil,
		},
	}

	// Parse in JSON
	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("failed to marshal settings to JSON: %w", err)
	}

	// Execute PUT _cluster/settings
	req := bytes.NewReader(data)
	res, err := es.Cluster.PutSettings(req)
	if err != nil {
		return fmt.Errorf("failed to update cluster settings: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("error updating cluster settings: %s", res.String())
	}

	return nil
}
