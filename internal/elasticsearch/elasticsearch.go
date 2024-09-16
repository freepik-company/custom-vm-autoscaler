package elasticsearch

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// DrainElasticsearchNode drains an Elasticsearch node and performs a controlled shutdown.
// elasticURL: The URL of the Elasticsearch cluster.
// nodeName: The name of the node to shut down.
// username: The username for basic authentication.
// password: The password for basic authentication.
func DrainElasticsearchNode(elasticURL, nodeName, username, password string) error {
	// Get Elasticsearch node IP
	nodeIP, err := getNodeIP(elasticURL, username, password)
	if err != nil {
		return fmt.Errorf("failed to get node IP: %w", err)
	}

	// Exclude the node from routing allocations
	err = updateClusterSettings(elasticURL, username, password, nodeIP)
	if err != nil {
		return fmt.Errorf("failed to update cluster settings: %w", err)
	}

	// Wait until the node is removed from the cluster
	err = waitForNodeRemoval(elasticURL, username, password)
	if err != nil {
		return fmt.Errorf("failed while waiting for node removal: %w", err)
	}

	return nil
}

// getNodeIP retrieves the IP address of the Elasticsearch node.
func getNodeIP(elasticURL, username, password string) (string, error) {
	cmd := exec.Command("curl", "-s", "-k", "-u", fmt.Sprintf("%s:%s", username, password), fmt.Sprintf("%s/_cat/nodes?v&h=ip,name", elasticURL))
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to execute curl command: %w", err)
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, getHostname()) {
			fields := strings.Fields(line)
			if len(fields) > 0 {
				return fields[0], nil
			}
		}
	}
	return "", fmt.Errorf("node IP not found")
}

// updateClusterSettings updates the cluster settings to exclude a specific node IP.
func updateClusterSettings(elasticURL, username, password, nodeIP string) error {
	data := fmt.Sprintf(`{
		"persistent": {
			"cluster.routing.allocation.exclude._ip": "%s"
		}
	}`, nodeIP)
	req, err := http.NewRequest("PUT", fmt.Sprintf("%s/_cluster/settings", elasticURL), bytes.NewBuffer([]byte(data)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.SetBasicAuth(username, password)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("error updating cluster settings: %s", string(body))
	}

	return nil
}

// waitForNodeRemoval waits for the node to be removed from the cluster.
func waitForNodeRemoval(elasticURL, username, password string) error {
	for {
		cmd := exec.Command("curl", "-s", "-k", "-u", fmt.Sprintf("%s:%s", username, password), fmt.Sprintf("%s/_cat/shards", elasticURL))
		output, err := cmd.Output()
		if err != nil {
			return fmt.Errorf("failed to execute curl command: %w", err)
		}

		if !strings.Contains(string(output), getHostname()) {
			break
		}
		time.Sleep(10 * time.Second)
	}
	return nil
}

// shutdownServices stops Docker and Nomad services.
func shutdownServices() error {
	cmd := exec.Command("sudo", "systemctl", "stop", "docker")
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to stop Docker: %w", err)
	}

	cmd = exec.Command("sudo", "systemctl", "stop", "nomad")
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to stop Nomad: %w", err)
	}

	time.Sleep(10 * time.Second)
	return nil
}

// clearClusterSettings removes the node exclusion from cluster settings.
func ClearElasticsearchClusterSettings(elasticURL, username, password string) error {
	data := `{
		"persistent": {
			"cluster.routing.allocation.exclude._ip": null
		}
	}`
	req, err := http.NewRequest("PUT", fmt.Sprintf("%s/_cluster/settings", elasticURL), bytes.NewBuffer([]byte(data)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.SetBasicAuth(username, password)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("error clearing cluster settings: %s", string(body))
	}

	return nil
}

// getHostname retrieves the current hostname of the node.
func getHostname() string {
	hostname, _ := exec.Command("hostname").Output()
	return strings.TrimSpace(string(hostname))
}
