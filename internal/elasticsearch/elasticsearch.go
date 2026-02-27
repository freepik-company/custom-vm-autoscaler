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
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
)

// newESClient creates a new Elasticsearch client from the context configuration.
func newESClient(ctx *v1alpha1.Context) (*elasticsearch.Client, error) {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: ctx.Config.Target.Elasticsearch.SSLInsecureSkipVerify,
			MinVersion:         tls.VersionTLS13,
		},
	}

	cfg := elasticsearch.Config{
		Addresses: []string{ctx.Config.Target.Elasticsearch.URL},
		Username:  ctx.Config.Target.Elasticsearch.User,
		Password:  ctx.Config.Target.Elasticsearch.Password,
		Transport: tr,
	}

	return elasticsearch.NewClient(cfg)
}

// DrainElasticsearchNode drains an Elasticsearch node and performs a controlled shutdown.
func DrainElasticsearchNode(ctx *v1alpha1.Context, nodeName string) error {

	es, err := newESClient(ctx)
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
	currentExcludes := ""
	ok := true
	if cluster, ok := currentSettings.Persistent["cluster"].(map[string]interface{}); ok {
		if routing, ok := cluster["routing"].(map[string]interface{}); ok {
			if allocation, ok := routing["allocation"].(map[string]interface{}); ok {
				if exclude, ok := allocation["exclude"].(map[string]interface{}); ok {
					if name, ok := exclude["_name"].(string); ok {
						currentExcludes = name
					}
				}
			}
		}
	}
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
		return fmt.Errorf("error compiling regex: %w", err)
	}

	// Create a context with timeout
	ctxWithTimeout, cancel := context.WithTimeout(context.Background(), time.Duration(ctx.Config.Target.Elasticsearch.DrainTimeoutSec)*time.Second)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctxWithTimeout.Done():
			if ctx.Config.Notifications.Slack.WebhookURL != "" {
				message := fmt.Sprintf("Timeout draining instance %s in elasticsearch. Timeout reached in %d seconds", nodeName, ctx.Config.Target.Elasticsearch.DrainTimeoutSec)
				if err := slack.NotifySlack(message, ctx.Config.Notifications.Slack.WebhookURL); err != nil {
					log.Printf("Error sending Slack notification: %v", err)
				}
			}

			// Add node again to the cluster settings
			err = ClearElasticsearchClusterSettings(ctx, nodeName)
			if err != nil {
				return fmt.Errorf("error clearing cluster settings: %w", err)
			}

			return fmt.Errorf("timeout trying to remove node from cluster settings in elasticsearch: %v", ctxWithTimeout.Err())

		case <-ticker.C:
			// Get _cat/shards to check if nodeName has any shard inside
			// Pass the timeout context so HTTP calls respect the deadline
			res, err := es.Cat.Shards(
				es.Cat.Shards.WithContext(ctxWithTimeout),
				es.Cat.Shards.WithFormat("json"),
				es.Cat.Shards.WithV(true),
			)
			if err != nil {
				// If context expired during the request, let the next select iteration handle timeout
				if ctxWithTimeout.Err() != nil {
					continue
				}
				return fmt.Errorf("failed to get shards information: %w", err)
			}

			// Get response
			body, err := io.ReadAll(res.Body)
			res.Body.Close()
			if err != nil || string(body) == "" {
				return fmt.Errorf("error reading response body: %w", err)
			}

			// Parse response in JSON
			var shards []v1alpha1.ShardInfo
			err = json.Unmarshal(body, &shards)
			if err != nil {
				return fmt.Errorf("error deserializing JSON: %w", err)
			}

			// Check if nodeName has any shards inside it
			nodeFound := false
			for _, shard := range shards {
				if re.MatchString(shard.Node) {
					nodeFound = true
					break
				}
			}

			// If nodeFound is false, there are not any shard inside it. It is ready to delete
			if !nodeFound {
				log.Printf("node %s is fully empty and ready to delete", nodeName)
				return nil
			}
		}
	}
}

// clearClusterSettings removes the node exclusion from cluster settings.
func ClearElasticsearchClusterSettings(ctx *v1alpha1.Context, nodeName string) error {

	es, err := newESClient(ctx)
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
	currentExcludes := ""
	ok := true
	if cluster, ok := currentSettings.Persistent["cluster"].(map[string]interface{}); ok {
		if routing, ok := cluster["routing"].(map[string]interface{}); ok {
			if allocation, ok := routing["allocation"].(map[string]interface{}); ok {
				if exclude, ok := allocation["exclude"].(map[string]interface{}); ok {
					if name, ok := exclude["_name"].(string); ok {
						currentExcludes = name
					}
				}
			}
		}
	}
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

// updateIndexReplicas updates the number_of_replicas setting for a given index.
func updateIndexReplicas(ctx *v1alpha1.Context, es *elasticsearch.Client, indexName string, replicas int) error {
	if ctx.Config.Autoscaler.DebugMode {
		log.Printf("Debug mode enabled. Skipping PUT %s/_settings with number_of_replicas=%d", indexName, replicas)
		return nil
	}

	settings := map[string]map[string]int{
		"index": {
			"number_of_replicas": replicas,
		},
	}

	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("failed to marshal settings: %w", err)
	}

	res, err := es.Indices.PutSettings(
		bytes.NewReader(data),
		es.Indices.PutSettings.WithIndex(indexName),
	)
	if err != nil {
		return fmt.Errorf("failed to update index settings for %s: %w", indexName, err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("error updating index settings for %s: %s", indexName, res.String())
	}

	return nil
}

// calculateDesiredReplicas computes the optimal number of replicas for a group of indices.
// totalPrimaries is the sum of primary shards across all indices in the group.
// The formula ensures totalPrimaries × (1 + replicas) >= nodeCount so no node is idle.
func calculateDesiredReplicas(nodeCount, totalPrimaries, maxReplicas, minReplicas int) int {
	if totalPrimaries <= 0 || nodeCount <= 0 {
		return minReplicas
	}

	desired := int(math.Ceil(float64(nodeCount)/float64(totalPrimaries))) - 1

	if desired < minReplicas {
		desired = minReplicas
	}
	if maxReplicas > 0 && desired > maxReplicas {
		desired = maxReplicas
	}

	return desired
}

// getIndicesForAliases resolves ES aliases to their underlying indices via _cat/aliases.
func getIndicesForAliases(es *elasticsearch.Client, aliases []string) ([]v1alpha1.IndexInfo, error) {
	if len(aliases) == 0 {
		return nil, nil
	}

	res, err := es.Cat.Aliases(
		es.Cat.Aliases.WithName(aliases...),
		es.Cat.Aliases.WithFormat("json"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get aliases: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("error getting aliases: %s", res.String())
	}

	var aliasInfos []v1alpha1.AliasInfo
	if err := json.NewDecoder(res.Body).Decode(&aliasInfos); err != nil {
		return nil, fmt.Errorf("failed to decode aliases response: %w", err)
	}

	// Collect unique index names from aliases
	indexNames := make([]string, 0, len(aliasInfos))
	seen := make(map[string]struct{})
	for _, a := range aliasInfos {
		if _, ok := seen[a.Index]; !ok {
			seen[a.Index] = struct{}{}
			indexNames = append(indexNames, a.Index)
		}
	}

	if len(indexNames) == 0 {
		return nil, nil
	}

	// Get full index info for resolved indices
	res, err = es.Cat.Indices(
		es.Cat.Indices.WithIndex(indexNames...),
		es.Cat.Indices.WithFormat("json"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get indices information: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("error getting indices: %s", res.String())
	}

	var indices []v1alpha1.IndexInfo
	if err := json.NewDecoder(res.Body).Decode(&indices); err != nil {
		return nil, fmt.Errorf("failed to decode indices response: %w", err)
	}

	return indices, nil
}

// getNodeCountForIndices returns the number of unique nodes hosting shards for the given indices.
// This ensures we only count nodes relevant to the filtered indices, not all data nodes in the cluster.
func getNodeCountForIndices(es *elasticsearch.Client, indexNames []string) (int, error) {
	if len(indexNames) == 0 {
		return 0, nil
	}

	res, err := es.Cat.Shards(
		es.Cat.Shards.WithIndex(indexNames...),
		es.Cat.Shards.WithFormat("json"),
	)
	if err != nil {
		return 0, fmt.Errorf("failed to get shards information: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return 0, fmt.Errorf("error getting shards: %s", res.String())
	}

	var shards []v1alpha1.ShardInfo
	if err := json.NewDecoder(res.Body).Decode(&shards); err != nil {
		return 0, fmt.Errorf("failed to decode shards response: %w", err)
	}

	uniqueNodes := make(map[string]struct{})
	for _, shard := range shards {
		if shard.Node != "" {
			uniqueNodes[shard.Node] = struct{}{}
		}
	}

	return len(uniqueNodes), nil
}

// RebalanceShards adjusts the number_of_replicas of indices to optimize shard distribution
// across the nodes hosting those indices. Returns the number of indices modified.
func RebalanceShards(ctx *v1alpha1.Context) (int, error) {
	cfg := ctx.Config.Target.Elasticsearch.ShardRebalancing

	es, err := newESClient(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to create Elasticsearch client: %w", err)
	}

	filtered, err := getIndicesForAliases(es, cfg.Aliases)
	if err != nil {
		return 0, fmt.Errorf("failed to resolve aliases: %w", err)
	}
	if len(filtered) == 0 {
		return 0, nil
	}

	// Collect index names to query shards only for relevant indices
	indexNames := make([]string, len(filtered))
	for i, idx := range filtered {
		indexNames[i] = idx.Index
	}

	nodeCount, err := getNodeCountForIndices(es, indexNames)
	if err != nil {
		return 0, fmt.Errorf("failed to get node count for indices: %w", err)
	}

	// Sum total primaries across all filtered indices
	totalPrimaries := 0
	for _, idx := range filtered {
		pri, err := strconv.Atoi(idx.Pri)
		if err != nil {
			log.Printf("Error parsing primaries for index %s: %v", idx.Index, err)
			continue
		}
		totalPrimaries += pri
	}

	desired := calculateDesiredReplicas(nodeCount, totalPrimaries, cfg.MaxReplicas, cfg.MinReplicas)

	log.Printf("Shard rebalancing: %d indices, %d total primaries, %d nodes -> desired replicas: %d",
		len(filtered), totalPrimaries, nodeCount, desired)

	modified := 0
	for _, idx := range filtered {
		currentReplicas, err := strconv.Atoi(idx.Rep)
		if err != nil {
			log.Printf("Error parsing replicas for index %s: %v", idx.Index, err)
			continue
		}

		if currentReplicas == desired {
			continue
		}

		log.Printf("Rebalancing index %s: changing replicas from %d to %d",
			idx.Index, currentReplicas, desired)

		if err := updateIndexReplicas(ctx, es, idx.Index, desired); err != nil {
			log.Printf("Error updating replicas for index %s: %v", idx.Index, err)
			continue
		}
		modified++
	}

	return modified, nil
}
