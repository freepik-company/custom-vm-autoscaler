package slack

import (
	"github.com/slack-go/slack"
)

// NotifySlack sends a message to a Slack channel using a webhook URL.
// message: The message to be sent to Slack.
// webhookURL: The Slack webhook URL used to post the message.
func NotifySlack(message, webhookURL string) error {
	// Create a Slack webhook message with the provided text
	msg := slack.WebhookMessage{
		Text: message,
	}

	// Post the message to Slack using the webhook URL
	return slack.PostWebhook(webhookURL, &msg)
}
