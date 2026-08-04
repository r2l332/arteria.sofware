package connectors

// ConnectorType defines the supported cloud/event-driven CP types.
type ConnectorType string

const (
	// Storage connectors (message archival)
	ConnectorS3        ConnectorType = "S3"
	ConnectorAzureBlob ConnectorType = "AZURE_BLOB"

	// Event/Queue connectors
	ConnectorSQS           ConnectorType = "SQS"
	ConnectorSNS           ConnectorType = "SNS"
	ConnectorAzureEventHub ConnectorType = "AZURE_EVENT_HUB"
	ConnectorAzureServiceBus ConnectorType = "AZURE_SERVICE_BUS"

	// HTTP connectors
	ConnectorWebhook ConnectorType = "WEBHOOK"

	// Traditional
	ConnectorMLLP ConnectorType = "MLLP"
	ConnectorTCP  ConnectorType = "TCP"
	ConnectorHTTP ConnectorType = "HTTP"
	ConnectorREST ConnectorType = "REST"
)

// ConnectorConfig holds cloud-specific configuration for a CP.
// Stored as JSON in communication_points.config_json
type ConnectorConfig struct {
	// S3 / Azure Blob
	BucketName      string `json:"bucket_name,omitempty"`
	ContainerName   string `json:"container_name,omitempty"`
	Region          string `json:"region,omitempty"`
	Prefix          string `json:"prefix,omitempty"`          // e.g., "hl7/inbound/"
	ConnectionString string `json:"connection_string,omitempty"`

	// SQS / SNS
	QueueURL  string `json:"queue_url,omitempty"`
	TopicARN  string `json:"topic_arn,omitempty"`
	AccessKey string `json:"access_key,omitempty"`
	SecretKey string `json:"secret_key,omitempty"`

	// Azure Event Hub / Service Bus
	EventHubName string `json:"event_hub_name,omitempty"`
	Namespace    string `json:"namespace,omitempty"`

	// Webhook
	WebhookURL     string            `json:"webhook_url,omitempty"`
	WebhookMethod  string            `json:"webhook_method,omitempty"`  // POST, PUT
	WebhookHeaders map[string]string `json:"webhook_headers,omitempty"`
	WebhookRetries int               `json:"webhook_retries,omitempty"`

	// Common
	Format        string `json:"format,omitempty"`        // "raw", "json", "fhir_bundle"
	BatchSize     int    `json:"batch_size,omitempty"`    // For batched sends
	FlushInterval int    `json:"flush_interval_ms,omitempty"`
}

// AllConnectorTypes returns all supported connector types grouped by category.
func AllConnectorTypes() map[string][]ConnectorType {
	return map[string][]ConnectorType{
		"traditional": {ConnectorMLLP, ConnectorTCP, ConnectorHTTP, ConnectorREST},
		"storage":     {ConnectorS3, ConnectorAzureBlob},
		"eventing":    {ConnectorSQS, ConnectorSNS, ConnectorAzureEventHub, ConnectorAzureServiceBus},
		"http":        {ConnectorWebhook},
	}
}

// ValidateConfig checks required fields for a given connector type.
func ValidateConfig(connType ConnectorType, cfg ConnectorConfig) []string {
	var errors []string

	switch connType {
	case ConnectorS3:
		if cfg.BucketName == "" {
			errors = append(errors, "bucket_name is required for S3")
		}
		if cfg.Region == "" {
			errors = append(errors, "region is required for S3")
		}
	case ConnectorAzureBlob:
		if cfg.ContainerName == "" {
			errors = append(errors, "container_name is required for Azure Blob")
		}
		if cfg.ConnectionString == "" {
			errors = append(errors, "connection_string is required for Azure Blob")
		}
	case ConnectorSQS:
		if cfg.QueueURL == "" {
			errors = append(errors, "queue_url is required for SQS")
		}
	case ConnectorSNS:
		if cfg.TopicARN == "" {
			errors = append(errors, "topic_arn is required for SNS")
		}
	case ConnectorAzureEventHub:
		if cfg.EventHubName == "" {
			errors = append(errors, "event_hub_name is required for Event Hub")
		}
		if cfg.ConnectionString == "" {
			errors = append(errors, "connection_string is required for Event Hub")
		}
	case ConnectorAzureServiceBus:
		if cfg.Namespace == "" {
			errors = append(errors, "namespace is required for Service Bus")
		}
		if cfg.ConnectionString == "" {
			errors = append(errors, "connection_string is required for Service Bus")
		}
	case ConnectorWebhook:
		if cfg.WebhookURL == "" {
			errors = append(errors, "webhook_url is required for Webhook")
		}
	}

	return errors
}
