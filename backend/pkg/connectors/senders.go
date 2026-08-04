package connectors

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Sender is the interface all output connectors implement.
type Sender interface {
	Send(payload []byte, metadata map[string]string) error
	Close() error
}

// --- Webhook Sender ---

type WebhookSender struct {
	url     string
	method  string
	headers map[string]string
	client  *http.Client
	retries int
}

func NewWebhookSender(cfg ConnectorConfig) *WebhookSender {
	method := cfg.WebhookMethod
	if method == "" {
		method = "POST"
	}
	retries := cfg.WebhookRetries
	if retries == 0 {
		retries = 3
	}
	return &WebhookSender{
		url:     cfg.WebhookURL,
		method:  method,
		headers: cfg.WebhookHeaders,
		retries: retries,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (w *WebhookSender) Send(payload []byte, metadata map[string]string) error {
	var lastErr error
	for attempt := 0; attempt <= w.retries; attempt++ {
		req, err := http.NewRequest(w.method, w.url, bytes.NewReader(payload))
		if err != nil {
			return fmt.Errorf("webhook request: %w", err)
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Arteria-Message-ID", metadata["message_id"])
		req.Header.Set("X-Arteria-Message-Type", metadata["message_type"])
		for k, v := range w.headers {
			req.Header.Set(k, v)
		}

		resp, err := w.client.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(time.Duration(attempt+1) * time.Second)
			continue
		}
		io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}
		lastErr = fmt.Errorf("webhook returned status %d", resp.StatusCode)
		time.Sleep(time.Duration(attempt+1) * time.Second)
	}
	return fmt.Errorf("webhook failed after %d retries: %w", w.retries, lastErr)
}

func (w *WebhookSender) Close() error { return nil }

// --- S3 Sender (uses pre-signed URLs or SDK — placeholder for now) ---

type S3Sender struct {
	bucket string
	prefix string
	region string
}

func NewS3Sender(cfg ConnectorConfig) *S3Sender {
	return &S3Sender{
		bucket: cfg.BucketName,
		prefix: cfg.Prefix,
		region: cfg.Region,
	}
}

func (s *S3Sender) Send(payload []byte, metadata map[string]string) error {
	// In production, this would use the AWS SDK:
	// s3.PutObject(bucket, prefix+messageID+".hl7", payload)
	// For now, log the intent
	_ = fmt.Sprintf("s3://%s/%s%s.hl7", s.bucket, s.prefix, metadata["message_id"])
	return fmt.Errorf("S3 sender: AWS SDK not yet integrated — install github.com/aws/aws-sdk-go-v2")
}

func (s *S3Sender) Close() error { return nil }

// --- Azure Blob Sender (placeholder) ---

type AzureBlobSender struct {
	container  string
	connString string
	prefix     string
}

func NewAzureBlobSender(cfg ConnectorConfig) *AzureBlobSender {
	return &AzureBlobSender{
		container:  cfg.ContainerName,
		connString: cfg.ConnectionString,
		prefix:     cfg.Prefix,
	}
}

func (a *AzureBlobSender) Send(payload []byte, metadata map[string]string) error {
	// In production: azblob.UploadBuffer(container, prefix+messageID+".hl7", payload)
	return fmt.Errorf("Azure Blob sender: SDK not yet integrated — install github.com/Azure/azure-sdk-for-go")
}

func (a *AzureBlobSender) Close() error { return nil }

// --- Factory ---

// NewSender creates the appropriate sender for a connector type.
func NewSender(connType ConnectorType, cfg ConnectorConfig) (Sender, error) {
	switch connType {
	case ConnectorWebhook:
		return NewWebhookSender(cfg), nil
	case ConnectorS3:
		return NewS3Sender(cfg), nil
	case ConnectorAzureBlob:
		return NewAzureBlobSender(cfg), nil
	default:
		return nil, fmt.Errorf("no sender implementation for connector type: %s", connType)
	}
}

// --- Message Archiver ---

// Archiver handles message archival to cloud storage.
type Archiver struct {
	sender   Sender
	format   string
}

func NewArchiver(connType ConnectorType, cfg ConnectorConfig) (*Archiver, error) {
	sender, err := NewSender(connType, cfg)
	if err != nil {
		return nil, err
	}
	format := cfg.Format
	if format == "" {
		format = "raw"
	}
	return &Archiver{sender: sender, format: format}, nil
}

func (a *Archiver) Archive(messageID, rawPayload string, metadata map[string]string) error {
	var payload []byte
	switch a.format {
	case "json":
		data := map[string]interface{}{
			"message_id": messageID,
			"payload":    rawPayload,
			"metadata":   metadata,
			"archived_at": time.Now().UTC().Format(time.RFC3339),
		}
		payload, _ = json.Marshal(data)
	default:
		payload = []byte(rawPayload)
	}
	return a.sender.Send(payload, metadata)
}

func (a *Archiver) Close() error {
	return a.sender.Close()
}
