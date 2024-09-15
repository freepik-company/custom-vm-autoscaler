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

	cooldownPeriodSeconds, _ := strconv.ParseInt(os.Getenv("COOLDOWN_PERIOD_SEC"), 10, 64)
	retryIntervalSeconds, _ := strconv.ParseInt(os.Getenv("RETRY_INTERVAL_SEC"), 10, 64)

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
			time.Sleep(time.Duration(retryIntervalSeconds) * time.Second)
			continue
		}

		if condition {
			log.Printf("Condition %s met: Creating new node!", prometheusCondition)
			err = google.AddNodeToMIG(projectID, zone, migName, debugMode)
			if err != nil {
				log.Printf("Error adding node to MIG: %v", err)
				time.Sleep(time.Duration(retryIntervalSeconds) * time.Second)
				continue
			}
			slack.NotifySlack("Added node to MIG", slackWebhookURL)
		} else {
			log.Printf("Condition %s not met. Removing one node!", prometheusCondition)
			err = google.RemoveNodeFromMIG(projectID, zone, migName, elasticURL, elasticUser, elasticPassword, debugMode)
			if err != nil {
				log.Printf("Error draining node from MIG: %v", err)
				time.Sleep(time.Duration(retryIntervalSeconds) * time.Second)
				continue
			}
			slack.NotifySlack("Removed node from MIG", slackWebhookURL)
		}
		// Check every 5 minutes
		time.Sleep(time.Duration(cooldownPeriodSeconds) * time.Second)
	}
}
