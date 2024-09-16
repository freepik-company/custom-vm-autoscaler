# Elasticsearch VM Autoscaler

This project is designed to automate the scaling of Google Cloud Managed Instance Groups (MIGs) based on Prometheus metrics and manage Elasticsearch nodes. It includes functionality for scaling up/down MIGs and draining Elasticsearch nodes safely.

## Overview

- **Scaling**: The application monitors Prometheus metrics to determine whether to scale the Managed Instance Group (MIG) up or down.
- **Elasticsearch Node Management**: Handles Elasticsearch nodes by excluding them from the cluster, waiting until they are removed, and then shutting down the services on those nodes.
- **Notification**: Sends notifications about scaling operations to Slack.

## Components

1. **`main.go`**: Entry point of the application. It handles the Prometheus query conditions, scaling of MIGs, and integrates Slack notifications.
2. **`google` package**: Contains functions for interacting with Google Cloud Compute Engine to manage MIGs.
3. **`prometheus` package**: Contains functions for querying Prometheus to evaluate conditions.
4. **`slack` package**: Contains functions for sending notifications to Slack.
5. **`elasticsearch` package**: Contains functions for managing Elasticsearch nodes, including draining and clearing cluster settings.

## Setup

1. **Environment Variables**:
   Ensure the following environment variables are set:

   ```sh
   PROMETHEUS_URL="http://your-prometheus-url"
   PROMETHEUS_UP_CONDITION="your-up-condition"
   PROMETHEUS_DOWN_CONDITION="your-down-condition"
   GCP_PROJECT_ID="your-gcp-project-id"
   ZONE="your-gcp-zone"
   MIG_NAME="your-mig-name"
   SLACK_WEBHOOK_URL="your-slack-webhook-url"
   ELASTIC_URL="https://your-elasticsearch-url"
   ELASTIC_USER="your-elasticsearch-user"
   ELASTIC_PASSWORD="your-elasticsearch-password"
   COOLDOWN_PERIOD_SEC="300"
   RETRY_INTERVAL_SEC="30"
   DEBUG_MODE="false"
   MIN_SIZE="1"
   MAX_SIZE="10"

2.	**Dependencies**:
	•	Go modules: Ensure you have Go installed and run go mod tidy to install dependencies.

3.	**Building**:
To build the application, use:
```
make build
```

4.	**Running**:
Execute the application:
```
./bin/autoscaler
```
## Packages

### main.go

The main application logic. It periodically checks Prometheus conditions, scales the MIG up or down based on these conditions, and sends notifications to Slack.

### google

Contains functions for Google Cloud operations:

* AddNodeToMIG: Increases the size of the MIG by 1 if it has not reached the maximum size.
* RemoveNodeFromMIG: Decreases the size of the MIG by 1 after draining an Elasticsearch node.
* CheckMIGMinimumSize: Ensures the MIG size is not below the minimum size.

### prometheus

Contains functions for querying Prometheus:

* GetPrometheusCondition: Executes a Prometheus query and checks if the condition is met.

### slack

Contains functions for sending Slack notifications:

* NotifySlack: Sends a notification message to a Slack channel using a webhook URL.

### elasticsearch

Contains functions for managing Elasticsearch nodes:

* DrainElasticsearchNode: Excludes a node from the cluster, waits for it to be removed, and then shuts down the node services.
* ClearElasticsearchClusterSettings: Clears the exclusion settings for the node from the cluster configuration.
* getNodeIP: Retrieves the IP address of the node.
* updateClusterSettings: Updates cluster settings to exclude a node by IP.
* waitForNodeRemoval: Waits until the node is removed from the cluster.
* shutdownServices: Stops Docker and Nomad services.
* getHostname: Retrieves the current hostname of the node.

## License

This project is licensed under the MIT License. See the LICENSE file for details.
### Explanation

- **Project Overview**: Provides a brief summary of the project and its functionality.
- **Setup**: Instructions for setting up environment variables, installing dependencies, building, and running the project.
- **Packages**: Details each package and its functionality, explaining how they contribute to the overall project.
- **License**: Indicates the licensing terms.

Feel free to modify the content as needed to better fit your project details or preferences.