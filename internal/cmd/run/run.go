package run

import (
	"custom-vm-autoscaler/api/v1alpha1"
	"custom-vm-autoscaler/internal/config"
	"custom-vm-autoscaler/internal/google"
	"custom-vm-autoscaler/internal/prometheus"
	"custom-vm-autoscaler/internal/slack"
	"fmt"

	"log"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const (
	descriptionShort = `Run the autoscaler`
	descriptionLong  = `
	Run the autoscaler with custom config file`
)

func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:                   "run",
		DisableFlagsInUseLine: true,
		Short:                 descriptionShort,
		Long:                  strings.ReplaceAll(descriptionLong, "\t", ""),

		Run: RunCommand,
	}

	cmd.Flags().String("config", "autoscaler.yaml", "Path to the YAML config file")

	return cmd
}

func RunCommand(cmd *cobra.Command, args []string) {

	// Check the flags for this command
	configPath, err := cmd.Flags().GetString("config")
	if err != nil {
		log.Fatalf("Error getting configuration file path: %v", err)
	}

	// Configure application's context
	ctx := v1alpha1.Context{
		Config: &v1alpha1.ConfigSpec{},
	}

	// Load default values
	ctx.Config.Service.Elasticsearch.SSLInsecureSkipVerify = defaultElasticsearchInsecureSkipVerify
	ctx.Config.Autoscaler.DebugMode = defaultDebugMode

	// Get and parse the config
	configContent, err := config.ReadFile(configPath)
	if err != nil {
		log.Fatalf("Error parsing configuration file: %v", err)
	}

	// Set the configuration inside the global context
	ctx.Config = &configContent

	// Main loop to monitor scaling conditions and manage the MIG
	for {

		// Check if the MIG is at its minimum size at least. If not, scale it up to minSize
		minSize, err := google.CheckMIGMinimumSize(&ctx)
		if err != nil {
			log.Printf("Error checking minimum size for MIG nodes: %v", err)
		} else {
			log.Printf("MIG %s scaled up to its minimum size %d", ctx.Config.Infrastructure.GCP.MIGName, minSize)
			if ctx.Config.Notifications.Slack.WebhookURL != "" {
				message := fmt.Sprintf("MIG %s scaled up to its minimum size %d", ctx.Config.Infrastructure.GCP.MIGName, minSize)
				err = slack.NotifySlack(message, ctx.Config.Notifications.Slack.WebhookURL)
				if err != nil {
					log.Printf("Error sending Slack notification: %v", err)
				}
			}
			time.Sleep(time.Duration(ctx.Config.Autoscaler.DefaultCooldownPeriodSec) * time.Second)
			continue
		}

		// Fetch the scale up and down conditions from Prometheus
		upCondition, err := prometheus.GetPrometheusCondition(ctx.Config.Metrics.Prometheus.UpCondition, &ctx)
		if err != nil {
			log.Printf("Error querying Prometheus: %v", err)
			time.Sleep(time.Duration(ctx.Config.Autoscaler.RetryIntervalSec) * time.Second)
			continue
		}

		downCondition, err := prometheus.GetPrometheusCondition(ctx.Config.Metrics.Prometheus.DownCondition, &ctx)
		if err != nil {
			log.Printf("Error querying Prometheus: %v", err)
			time.Sleep(time.Duration(ctx.Config.Autoscaler.RetryIntervalSec) * time.Second)
			continue
		}

		// If the up condition is met, add a node to the MIG
		if upCondition {
			log.Printf("Up condition %s met: Trying to create a new node!", ctx.Config.Metrics.Prometheus.UpCondition)
			currentSize, maxSize, err := google.AddNodeToMIG(&ctx)
			if err != nil {
				log.Printf("Error adding node to MIG: %v", err)
				time.Sleep(time.Duration(ctx.Config.Autoscaler.RetryIntervalSec) * time.Second)
				continue
			}
			// Notify via Slack that a node has been added
			if ctx.Config.Notifications.Slack.WebhookURL != "" {
				message := fmt.Sprintf("Added new node to MIG %s. Current size is %d nodes and the maximum nodes to create are %d", ctx.Config.Infrastructure.GCP.MIGName, currentSize, maxSize)
				err = slack.NotifySlack(message, ctx.Config.Notifications.Slack.WebhookURL)
				if err != nil {
					log.Printf("Error sending Slack notification: %v", err)
				}
			}
			// Sleep for the default cooldown period before checking the conditions again
			time.Sleep(time.Duration(ctx.Config.Autoscaler.DefaultCooldownPeriodSec) * time.Second)
		} else if downCondition { // If the down condition is met, remove a node from the MIG
			log.Printf("Down condition %s met. Trying to remove one node!", ctx.Config.Metrics.Prometheus.DownCondition)
			currentSize, minSize, nodeRemoved, err := google.RemoveNodeFromMIG(&ctx)
			if err != nil {
				log.Printf("Error draining node from MIG: %v", err)
				time.Sleep(time.Duration(ctx.Config.Autoscaler.RetryIntervalSec) * time.Second)
				continue
			}
			// Notify via Slack that a node has been removed
			if ctx.Config.Notifications.Slack.WebhookURL != "" {
				message := fmt.Sprintf("Removed node %s from MIG %s. Current size is %d nodes and the minimum nodes to exist are %d", nodeRemoved, ctx.Config.Infrastructure.GCP.MIGName, currentSize, minSize)
				err = slack.NotifySlack(message, ctx.Config.Notifications.Slack.WebhookURL)
				if err != nil {
					log.Printf("Error sending Slack notification: %v", err)
				}
			}
			// Sleep for the scaledown cooldown period before checking the conditions again
			time.Sleep(time.Duration(ctx.Config.Autoscaler.ScaleDownCooldownPeriodSec) * time.Second)
		} else {
			// No scaling conditions met, so no changes to the MIG
			log.Printf("No condition %s or %s met, keeping the same number of nodes!", ctx.Config.Metrics.Prometheus.UpCondition, ctx.Config.Metrics.Prometheus.DownCondition)
			// Sleep for the default cooldown period before checking the conditions again
			time.Sleep(time.Duration(ctx.Config.Autoscaler.DefaultCooldownPeriodSec) * time.Second)
		}
	}
}
