package elasticsearch

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
)

// nodeInfo struct for elasticsearch nodes
type NodeInfo struct {
	IP          string `json:"ip"`
	HeapPercent string `json:"heap.percent"`
	RAMPercent  string `json:"ram.percent"`
	CPU         string `json:"cpu"`
	Load1m      string `json:"load_1m"`
	Load5m      string `json:"load_5m"`
	Load15m     string `json:"load_15m"`
	NodeRole    string `json:"node.role"`
	Master      string `json:"master"`
	Name        string `json:"name"`
}

// DrainElasticsearchNode drains an Elasticsearch node and performs a controlled shutdown.
// elasticURL: The URL of the Elasticsearch cluster.
// nodeName: The name of the node to shut down.
// username: The username for basic authentication.
// password: The password for basic authentication.
func DrainElasticsearchNode(elasticURL, nodeName, username, password string) error {

	// Configurar http.Transport para desactivar la verificación del certificado
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	cfg := elasticsearch.Config{
		Addresses: []string{elasticURL},
		Username:  username,
		Password:  password,
		Transport: tr,
	}

	es, err := elasticsearch.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("failed to create Elasticsearch client: %w", err)
	}

	// Get Elasticsearch node IP
	nodeIP, err := getNodeIP(es, nodeName)
	if err != nil {
		return fmt.Errorf("failed to get node IP: %w", err)
	}

	// Exclude the node from routing allocations
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

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return "", fmt.Errorf("error reading response body: %w", err)
	}

	var nodes []NodeInfo
	err = json.Unmarshal([]byte(string(body)), &nodes)
	if err != nil {
		return "", fmt.Errorf("error deserializing JSON: %w", err)
	}

	// Find the IP address for the node with the hostname
	for _, node := range nodes {
		if node.Name == nodeName {
			return node.IP, nil
		}
	}

	return "", fmt.Errorf("node IP not found")
}

// updateClusterSettings updates the cluster settings to exclude a specific node IP.
func updateClusterSettings(es *elasticsearch.Client, nodeIP string) error {

	settings := map[string]map[string]string{
		"persistent": {
			"cluster.routing.allocation.exclude._ip": nodeIP,
		},
	}

	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("failed to marshal settings to JSON: %w", err)
	}

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

	for {
		res, err := es.Cat.Shards(
			es.Cat.Shards.WithFormat("json"),
		)
		if err != nil {
			return fmt.Errorf("failed to get shards information: %w", err)
		}
		defer res.Body.Close()

		var shards []map[string]interface{}
		if err := json.NewDecoder(res.Body).Decode(&shards); err != nil {
			return fmt.Errorf("failed to decode shards information: %w", err)
		}

		nodeFound := false
		for _, shard := range shards {
			// Assuming `node` field contains the node name
			if node, ok := shard["node"].(string); ok && strings.Contains(node, nodeName) {
				nodeFound = true
				break
			}
		}

		if !nodeFound {
			break
		}

		time.Sleep(10 * time.Second)
	}

	return nil
}

// clearClusterSettings removes the node exclusion from cluster settings.
func ClearElasticsearchClusterSettings(elasticURL, username, password string) error {
	// Configurar http.Transport para desactivar la verificación del certificado
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	cfg := elasticsearch.Config{
		Addresses: []string{elasticURL},
		Username:  username,
		Password:  password,
		Transport: tr,
	}

	es, err := elasticsearch.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("failed to create Elasticsearch client: %w", err)
	}

	settings := map[string]map[string]string{
		"persistent": {
			"cluster.routing.allocation.exclude._ip": "",
		},
	}

	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("failed to marshal settings to JSON: %w", err)
	}

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
