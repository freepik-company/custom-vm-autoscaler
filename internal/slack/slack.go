package slack

import (
	"github.com/slack-go/slack"
)

func NotifySlack(message, webhookURL string) error {
	msg := slack.WebhookMessage{
		Text: message,
	}
	return slack.PostWebhook(webhookURL, &msg)
}
