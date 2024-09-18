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
   You can configure the following environment variables:
   * PROMETHEUS_URL: Prometheus to query about metrics for scaling (Default `http://localhost:9200`)
   * PROMETHEUS_UP_CONDITION: Prometheus query that must met to scale up the nodegroup. Program just check if the condition is true or false, do not check values (`Required`)
   * PROMETHEUS_DOWN_CONDITION:  Prometheus query that must met to scale down the nodegroup.  Program just check if the condition is true or false, do not check values (`Required`)
   * PROMETHEUS_HEADER_*: Prometheus http headers for queries. For example, PROMETHEUS_HEADER_X_Scope_OrgID environment variable adds a HTTP header called X-Scope-OrgID to the http request (`Optional`)
   * GCP_PROJECT_ID: Google Cloud project id (Default `example`)
   * ZONE: Google Cloud project zone (Default `europe-west1-d`)
   * MIG_NAME: Google Cloud MIG to scale (Default `example`)
   * GOOGLE_APPLICATION_CREDENTIALS: Google Cloud service account credentials json path (`Optional`)
   * SLACK_WEBHOOK_URL: Slack webhook to send messages about MIG scalation (`Optional`)
   * ELASTIC_URL: Elasticsearch URL to drain nodes (Default `http://elasticsearch:9200`)
   * ELASTIC_USER: Elasticsearch user for authentication (Default `elastic`)
   * ELASTIC_PASSWORD: Elasticsearch password for authentication (Default `password`)
   * ELASTIC_SSL_INSECURE_SKIP_VERIFY: Elasticsearch SSL certificate skip validation (Default `false`)
   * COOLDOWN_PERIOD_SEC: Cooldown seconds to wait between scale checks (Default `60`)
   * RETRY_INTERVAL_SEC: Retry timeout when an error is reached during the loop (Default `60`)
   * DEBUG_MODE: Does not execute scalations, just log and send slack messages (Default `false`)
   * MIN_SIZE: Minimum size for the nodegroup (Default `1`)
   * MAX_SIZE: Maximum size for the nodegroup (Default `1`)

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

### globals

Contains global functions

* GetEnv: Return environment variable value if set. If not, it returns the default value set as second argument

## License

This project is licensed under the MIT License. See the LICENSE file for details.
### Explanation

- **Project Overview**: Provides a brief summary of the project and its functionality.
- **Setup**: Instructions for setting up environment variables, installing dependencies, building, and running the project.
- **Packages**: Details each package and its functionality, explaining how they contribute to the overall project.
- **License**: Indicates the licensing terms.

Feel free to modify the content as needed to better fit your project details or preferences.