package main

import (
	"elasticsearch-vm-autoscaler/internal/globals"
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
	prometheusURL := globals.GetEnv("PROMETHEUS_URL", "http://localhost:9090")
	// Conditions for scaling up or down the MIG
	prometheusUpCondition := os.Getenv("PROMETHEUS_UP_CONDITION")
	if prometheusUpCondition == "" {
		log.Fatalf("You must set PROMETHEUS_UP_CONDITION environment variable with a PromQL query for scale up nodes")
	}
	prometheusDownCondition := os.Getenv("PROMETHEUS_DOWN_CONDITION")
	if prometheusDownCondition == "" {
		log.Fatalf("You must set PROMETHEUS_DOWN_CONDITION environment variable with a PromQL query for scale down nodes")
	}

	// Google Cloud MIG (Managed Instance Group) variables
	projectID := globals.GetEnv("GCP_PROJECT_ID", "example")
	zone := globals.GetEnv("ZONE", "europe-west1-d")
	migName := globals.GetEnv("MIG_NAME", "example")

	// Slack Webhook URL for notifications
	slackWebhookURL := os.Getenv("SLACK_WEBHOOK_URL")

	// Elasticsearch authentication details
	elasticURL := globals.GetEnv("ELASTIC_URL", "http://localhost:9200")
	elasticUser := globals.GetEnv("ELASTIC_USER", "elastic")
	elasticPassword := globals.GetEnv("ELASTIC_PASSWORD", "password")

	// Cooldown and retry intervals in seconds, parsed from environment variables
	cooldownPeriodSeconds, _ := strconv.ParseInt(globals.GetEnv("COOLDOWN_PERIOD_SEC", "60"), 10, 64)
	retryIntervalSeconds, _ := strconv.ParseInt(globals.GetEnv("RETRY_INTERVAL_SEC", "60"), 10, 64)

	// Debug mode flag, enabled if "DEBUG_MODE" is set to "true"
	debugModeStr := globals.GetEnv("DEBUG_MODE", "false")
	var debugMode bool
	if strings.ToLower(debugModeStr) == "true" {
		debugMode = true
	} else {
		debugMode = false
	}

	// Main loop to monitor scaling conditions and manage the MIG
	for {
		// Check if the MIG is at its minimum size at least. If not, scale it up to minSize
		err := google.CheckMIGMinimumSize(projectID, zone, migName, debugMode)
		if err != nil {
			log.Printf("Error checking minimum size for MIG nodes: %v", err)
		}

		// Fetch the scale up and down conditions from Prometheus
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
			log.Printf("Up condition %s met: Trying to create a new node!", prometheusUpCondition)
			err = google.AddNodeToMIG(projectID, zone, migName, debugMode)
			if err != nil {
				log.Printf("Error adding node to MIG: %v", err)
				time.Sleep(time.Duration(retryIntervalSeconds) * time.Second)
				continue
			}
			// Notify via Slack that a node has been added
			if slackWebhookURL != "" {
				slack.NotifySlack("New node created succesfully in MIG", slackWebhookURL)
			}
		} else if downCondition { // If the down condition is met, remove a node from the MIG
			log.Printf("Down condition %s met. Trying to remove one node!", prometheusDownCondition)
			err = google.RemoveNodeFromMIG(projectID, zone, migName, elasticURL, elasticUser, elasticPassword, debugMode)
			if err != nil {
				log.Printf("Error draining node from MIG: %v", err)
				time.Sleep(time.Duration(retryIntervalSeconds) * time.Second)
				continue
			}
			// Notify via Slack that a node has been removed
			if slackWebhookURL != "" {
				slack.NotifySlack("Removed node from MIG", slackWebhookURL)
			}
		} else {
			// No scaling conditions met, so no changes to the MIG
			log.Printf("No condition %s or %s met, keeping the same number of nodes!", prometheusUpCondition, prometheusDownCondition)
		}

		// Sleep for the cooldown period before checking the conditions again
		time.Sleep(time.Duration(cooldownPeriodSeconds) * time.Second)
	}
}
