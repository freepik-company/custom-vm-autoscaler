package elasticsearch

import (
	"bytes"
	"context"
	"crypto/tls"
	"custom-vm-autoscaler/api/v1alpha1"
	"custom-vm-autoscaler/internal/slack"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

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
			InsecureSkipVerify: ctx.Config.Target.Elasticsearch.SSLInsecureSkipVerify,
			MinVersion:         tls.VersionTLS13,
		},
	}

	// Create elasticsearch config for connection
	cfg := elasticsearch.Config{
		Addresses: []string{ctx.Config.Target.Elasticsearch.URL},
		Username:  ctx.Config.Target.Elasticsearch.User,
		Password:  ctx.Config.Target.Elasticsearch.Password,
		Transport: tr,
	}

	// Creates new client
	es, err := elasticsearch.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("failed to create Elasticsearch client: %w", err)
	}

	// Exclude the node IP from routing allocations
	err = updateClusterSettings(ctx, es, nodeName)
	if err != nil {
		return fmt.Errorf("failed to update cluster settings: %w", err)
	}

	// Wait until the node is removed from the cluster
	if !ctx.Config.Autoscaler.DebugMode {
		err = waitForNodeRemoval(ctx, es, nodeName)
		if err != nil {
			return fmt.Errorf("failed while waiting for node removal: %w", err)
		}
	}

	return nil
}

// updateClusterSettings updates the cluster settings to exclude a specific node IP.
func updateClusterSettings(ctx *v1alpha1.Context, es *elasticsearch.Client, nodeName string) error {

	// Get current cluster settings
	res, err := es.Cluster.GetSettings()
	if err != nil {
		return fmt.Errorf("failed to get current cluster settings: %w", err)
	}
	defer res.Body.Close()

	// decode response
	var currentSettings v1alpha1.ElasticsearchSettings
	if err := json.NewDecoder(res.Body).Decode(&currentSettings); err != nil {
		return fmt.Errorf("failed to decode cluster settings response: %w", err)
	}

	// Check current exclude IPs

	currentExcludes, ok := currentSettings.Persistent["cluster"].(map[string]interface{})["routing"].(map[string]interface{})["allocation"].(map[string]interface{})["exclude"].(map[string]interface{})["_name"].(string)

	if ctx.Config.Autoscaler.DebugMode {
		log.Printf("Debug mode enabled. Current nodes in exclude settings elasticsearch: %s", string(currentExcludes))
	}

	if ok && currentExcludes != "" {
		excludedNames := strings.Split(currentExcludes, ",")
		for _, name := range excludedNames {
			if name == nodeName {
				// IP already excluded, not needed to update
				fmt.Println("Node IP is already excluded from allocation")
				return nil
			}
		}
		// If the IP is not in the list, add it
		nodeName = currentExcludes + "," + nodeName
	}

	// _cluster/settings to set
	settings := map[string]map[string]string{
		"persistent": {
			"cluster.routing.allocation.exclude._name": nodeName,
		},
	}

	// Parse settings in JSON
	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("failed to marshal settings to JSON: %w", err)
	}

	if ctx.Config.Autoscaler.DebugMode {
		log.Printf("Debug mode enabled. Skipping PUT _cluster/settings command. Command to execute: %s", string(data))
	}

	// Execute PUT _cluster/settings command
	if !ctx.Config.Autoscaler.DebugMode {
		req := bytes.NewReader(data)
		res, err = es.Cluster.PutSettings(req)
		if err != nil {
			return fmt.Errorf("failed to update cluster settings: %w", err)
		}
		defer res.Body.Close()

		if res.IsError() {
			return fmt.Errorf("error updating cluster settings: %s", res.String())
		}
	}

	return nil
}

// waitForNodeRemoval waits for the node to be removed from the cluster.
func waitForNodeRemoval(ctx *v1alpha1.Context, es *elasticsearch.Client, nodeName string) error {

	// Prepare regex to match shards with
	re, err := regexp.Compile(nodeName)
	if err != nil {
		log.Fatalf("Error compiling regex: %v", err)
	}

	// Create a context with timeout
	ctxWithTimeout, cancel := context.WithTimeout(context.Background(), time.Duration(ctx.Config.Target.Elasticsearch.DrainTimeoutSec)*time.Second)
	defer cancel()

	for {

		// Check if context is done for timeout
		select {
		case <-ctxWithTimeout.Done():
			if ctx.Config.Notifications.Slack.WebhookURL != "" {
				message := fmt.Sprintf("Timeout draining instance %s in elasticsearch. Timeout reached in %d seconds", nodeName, ctx.Config.Target.Elasticsearch.DrainTimeoutSec)
				err = slack.NotifySlack(message, ctx.Config.Notifications.Slack.WebhookURL)
				if err != nil {
					log.Printf("Error sending Slack notification: %v", err)
				}
			}
			return fmt.Errorf("timeout trying to remove node from cluster settings in elasticsearch: %v", ctxWithTimeout.Err())
		default:
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
				return nil
			}

			// Sleep a brief period before next check to avoid excessive requests
			time.Sleep(2 * time.Second)
		}

	}

}

// clearClusterSettings removes the node exclusion from cluster settings.
func ClearElasticsearchClusterSettings(ctx *v1alpha1.Context, nodeName string) error {

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: ctx.Config.Target.Elasticsearch.SSLInsecureSkipVerify,
			MinVersion:         tls.VersionTLS13,
		},
	}

	// Configure elasticsearch connection
	cfg := elasticsearch.Config{
		Addresses: []string{ctx.Config.Target.Elasticsearch.URL},
		Username:  ctx.Config.Target.Elasticsearch.User,
		Password:  ctx.Config.Target.Elasticsearch.Password,
		Transport: tr,
	}

	// Create elastic client
	es, err := elasticsearch.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("failed to create Elasticsearch client: %w", err)
	}

	// Get current cluster settings
	res, err := es.Cluster.GetSettings()
	if err != nil {
		return fmt.Errorf("failed to get current cluster settings: %w", err)
	}
	defer res.Body.Close()

	// decode response
	var currentSettings v1alpha1.ElasticsearchSettings
	if err := json.NewDecoder(res.Body).Decode(&currentSettings); err != nil {
		return fmt.Errorf("failed to decode cluster settings response: %w", err)
	}

	// Get current excluded Names
	currentExcludes, ok := currentSettings.Persistent["cluster"].(map[string]interface{})["routing"].(map[string]interface{})["allocation"].(map[string]interface{})["exclude"].(map[string]interface{})["_name"].(string)

	if ctx.Config.Autoscaler.DebugMode {
		log.Printf("Debug mode enabled. Current nodes in exclude settings elasticsearch: %s", string(currentExcludes))
	}

	if !ok || currentExcludes == "" {
		fmt.Println("No names are currently excluded.")
		return nil
	}

	// Create a new list of excluded names without the node to be removed
	excludedNames := strings.Split(currentExcludes, ",")
	remainingNames := []string{}
	for _, name := range excludedNames {
		if name != nodeName {
			remainingNames = append(remainingNames, name)
		}
	}

	// Prepare configuration to update
	var newExcludes any
	if len(remainingNames) > 0 {
		newExcludes = strings.Join(remainingNames, ",")
	} else {
		newExcludes = nil
	}

	// _cluster/settings to set after the node deletion
	settings := map[string]map[string]any{
		"persistent": {
			"cluster.routing.allocation.exclude._name": newExcludes,
		},
	}

	// Parse in JSON
	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("failed to marshal settings to JSON: %w", err)
	}

	if ctx.Config.Autoscaler.DebugMode {
		log.Printf("Debug mode enabled. Skipping PUT _cluster/settings command. Command to execute: %s", string(data))
	}

	// Execute PUT _cluster/settings
	if !ctx.Config.Autoscaler.DebugMode {
		req := bytes.NewReader(data)
		res, err = es.Cluster.PutSettings(req)
		if err != nil {
			return fmt.Errorf("failed to update cluster settings: %w", err)
		}
		defer res.Body.Close()

		if res.IsError() {
			return fmt.Errorf("error updating cluster settings: %s", res.String())
		}
	}

	return nil
}
