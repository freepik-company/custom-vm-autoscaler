package v1alpha1

// Configuration struct
type ConfigSpec struct {
	Metrics struct {
		Prometheus struct {
			URL           string            `yaml:"url"`
			UpCondition   string            `yaml:"upCondition"`
			DownCondition string            `yaml:"downCondition"`
			Headers       map[string]string `yaml:"headers,omitempty"`
		} `yaml:"prometheus"`
	} `yaml:"metrics"`

	Infrastructure struct {
		GCP struct {
			ProjectID       string `yaml:"projectId"`
			Zone            string `yaml:"zone"`
			MIGName         string `yaml:"migName"`
			CredentialsFile string `yaml:"credentialsFile,omitempty"`
		} `yaml:"gcp"`
	} `yaml:"infrastructure"`

	Service struct {
		Elasticsearch struct {
			URL                   string `yaml:"url"`
			User                  string `yaml:"user"`
			Password              string `yaml:"password"`
			SSLInsecureSkipVerify bool   `yaml:"sslInsecureSkipVerify,omitempty"`
		} `yaml:"elasticsearch"`
	} `yaml:"service"`

	Notifications struct {
		Slack struct {
			WebhookURL string `yaml:"webhookUrl,omitempty"`
		} `yaml:"slack,omitempty"`
	} `yaml:"notifications,omitempty"`

	Autoscaler struct {
		DebugMode                          bool `yaml:"debugMode,omitempty"`
		DefaultCooldownPeriodSec           int  `yaml:"defaultCooldownPeriodSec"`
		ScaleDownCooldownPeriodSec         int  `yaml:"scaledownCooldownPeriodSec"`
		RetryIntervalSec                   int  `yaml:"retryIntervalSec"`
		MinSize                            int  `yaml:"minSize"`
		MaxSize                            int  `yaml:"maxSize"`
		AdvancedCustomScalingConfiguration []struct {
			Days     string `yaml:"days"`
			HoursUTC string `yaml:"hoursUTC,omitempty"`
			MinSize  int    `yaml:"minSize"`
			MaxSize  int    `yaml:"maxSize"`
		} `yaml:"advanzedCustomScalingConfiguration,omitempty"`
	} `yaml:"autoscaler"`
}
