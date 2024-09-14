package main

import (
	"elasticsearch-vm-autoscaler/internal/elasticsearch"
	mig "elasticsearch-vm-autoscaler/internal/google-mig"
	"elasticsearch-vm-autoscaler/internal/prometheus"
	"elasticsearch-vm-autoscaler/internal/slack"
	"log"
	"os"
	"strings"
	"time"
)

func main() {
	// Prometheus variables
	prometheusURL := os.Getenv("PROMETHEUS_URL")
	// If the condition mets, we try to create another node
	prometheusCondition := os.Getenv("PROMETHEUS_CONDITION")

	// Google MIG variables
	projectID := os.Getenv("GCP_PROJECT_ID")
	zone := os.Getenv("ZONE")
	migName := os.Getenv("MIG_NAME")

	slackWebhookURL := os.Getenv("SLACK_WEBHOOK_URL")

	elasticURL := os.Getenv("ELASTIC_URL")
	elasticUser := os.Getenv("ELASTIC_USER")
	elasticPassword := os.Getenv("ELASTIC_PASSWORD")

	// Other variables
	debugModeStr := os.Getenv("DEBUG_MODE")
	// Convierte el valor a booleano
	var debugMode bool
	if strings.ToLower(debugModeStr) == "true" {
		debugMode = true
	} else {
		debugMode = false
	}

	for {
		condition, err := prometheus.GetPrometheusCondition(prometheusURL, prometheusCondition)
		if err != nil {
			log.Printf("Error querying Prometheus: %v", err)

			// Retry after 5 seconds
			time.Sleep(5 * time.Second)
			continue
		}

		if condition {
			log.Printf("Condition %s met: Creating new node!", prometheusCondition)
			if !debugMode {
				err = mig.AddNodeToMIG(projectID, zone, migName)
				if err != nil {
					log.Printf("Error adding node to MIG: %v", err)
					continue
				}
				slack.NotifySlack("Removed node from MIG", slackWebhookURL)
			}
		} else {
			log.Printf("Condition %s not met. Removing one node!", prometheusCondition)
			if !debugMode {

				instanceToRemove, err := mig.GetInstanceToRemove(projectID, zone, migName)
				if err != nil {
					log.Printf("Error getting instance to remove: %v", err)
					continue
				}
				err = elasticsearch.DrainElasticsearchNode(elasticURL, instanceToRemove, elasticUser, elasticPassword)
				if err != nil {
					log.Printf("Error draining node in Elasticsearch: %v", err)
					continue
				}
				err = mig.RemoveNodeFromMIG(projectID, zone, migName, instanceToRemove)
				if err != nil {
					log.Printf("Error draining node from MIG: %v", err)
					continue
				}
			}
		}
		// Check every 5 minutes
		time.Sleep(5 * time.Minute)
	}
}
