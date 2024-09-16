package main

import (
	"elasticsearch-vm-autoscaler/internal/google"
	"elasticsearch-vm-autoscaler/internal/prometheus"
	"elasticsearch-vm-autoscaler/internal/slack"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

func main() {
	// Prometheus variables to hold configuration for scaling conditions
	prometheusURL := os.Getenv("PROMETHEUS_URL")
	// Conditions for scaling up or down the MIG
	prometheusUpCondition := os.Getenv("PROMETHEUS_UP_CONDITION")
	prometheusDownCondition := os.Getenv("PROMETHEUS_DOWN_CONDITION")

	// Google Cloud MIG (Managed Instance Group) variables
	projectID := os.Getenv("GCP_PROJECT_ID")
	zone := os.Getenv("ZONE")
	migName := os.Getenv("MIG_NAME")

	// Slack Webhook URL for notifications
	slackWebhookURL := os.Getenv("SLACK_WEBHOOK_URL")

	// Elasticsearch authentication details
	elasticURL := os.Getenv("ELASTIC_URL")
	elasticUser := os.Getenv("ELASTIC_USER")
	elasticPassword := os.Getenv("ELASTIC_PASSWORD")

	// Cooldown and retry intervals in seconds, parsed from environment variables
	cooldownPeriodSeconds, _ := strconv.ParseInt(os.Getenv("COOLDOWN_PERIOD_SEC"), 10, 64)
	retryIntervalSeconds, _ := strconv.ParseInt(os.Getenv("RETRY_INTERVAL_SEC"), 10, 64)

	// Debug mode flag, enabled if "DEBUG_MODE" is set to "true"
	debugModeStr := os.Getenv("DEBUG_MODE")
	var debugMode bool
	if strings.ToLower(debugModeStr) == "true" {
		debugMode = true
	} else {
		debugMode = false
	}

	// Main loop to monitor scaling conditions and manage the MIG
	for {
		// Check if the MIG is at its minimum size
		err := google.CheckMIGMinimumSize(projectID, zone, migName, debugMode)
		if err != nil {
			log.Printf("Error checking minimum size for MIG nodes: %v", err)
		}

		// Fetch the up and down conditions from Prometheus
		upCondition, err := prometheus.GetPrometheusCondition(prometheusURL, prometheusUpCondition)
		if err != nil {
			log.Printf("Error querying Prometheus: %v", err)
			time.Sleep(time.Duration(retryIntervalSeconds) * time.Second)
			continue
		}
		downCondition, err := prometheus.GetPrometheusCondition(prometheusURL, prometheusDownCondition)
		if err != nil {
			log.Printf("Error querying Prometheus: %v", err)
			time.Sleep(time.Duration(retryIntervalSeconds) * time.Second)
			continue
		}

		// If the up condition is met, add a node to the MIG
		if upCondition {
			log.Printf("Condition %s met: Creating new node!", prometheusUpCondition)
			err = google.AddNodeToMIG(projectID, zone, migName, debugMode)
			if err != nil {
				log.Printf("Error adding node to MIG: %v", err)
				time.Sleep(time.Duration(retryIntervalSeconds) * time.Second)
				continue
			}
			// Notify via Slack that a node has been added
			slack.NotifySlack("Added node to MIG", slackWebhookURL)
		} else if downCondition { // If the down condition is met, remove a node from the MIG
			log.Printf("Condition %s met. Removing one node!", prometheusDownCondition)
			err = google.RemoveNodeFromMIG(projectID, zone, migName, elasticURL, elasticUser, elasticPassword, debugMode)
			if err != nil {
				log.Printf("Error draining node from MIG: %v", err)
				time.Sleep(time.Duration(retryIntervalSeconds) * time.Second)
				continue
			}
			// Notify via Slack that a node has been removed
			slack.NotifySlack("Removed node from MIG", slackWebhookURL)
		} else {
			// No scaling conditions met, so no changes to the MIG
			log.Printf("No condition %s or %s met, keeping the same number of nodes!", prometheusUpCondition, prometheusDownCondition)
		}

		// Sleep for the cooldown period before checking the conditions again
		time.Sleep(time.Duration(cooldownPeriodSeconds) * time.Second)
	}
}
