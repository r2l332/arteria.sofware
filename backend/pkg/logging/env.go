package logging

import (
	"os"
	"strings"
)

// FromEnv builds a Logger from standard environment variables:
//   - LOG_LEVEL: TRACE, DEBUG, INFO, WARN, ERROR, FATAL (default: INFO)
//   - LOG_SINKS: comma-separated list of "stdout", "file", "http" (default: stdout)
//   - LOG_FILE: file path for file sink (default: /var/log/arteria/<service>.log)
//   - LOG_FILE_MAX_MB: max file size in MB before rotation (default: 100)
//   - LOG_HTTP_URL: endpoint for HTTP sink (e.g., http://loki:3100/loki/api/v1/push)
//   - LOG_HTTP_HEADERS: comma-separated key=value pairs for HTTP sink headers
func FromEnv(service string) (*Logger, error) {
	level := envOr("LOG_LEVEL", "INFO")
	sinksStr := envOr("LOG_SINKS", "stdout")
	filePath := envOr("LOG_FILE", "/var/log/arteria/"+service+".log")
	httpURL := os.Getenv("LOG_HTTP_URL")
	httpHeadersStr := os.Getenv("LOG_HTTP_HEADERS")

	var sinkConfigs []SinkConfig

	for _, s := range strings.Split(sinksStr, ",") {
		s = strings.TrimSpace(s)
		switch s {
		case "stdout":
			sinkConfigs = append(sinkConfigs, SinkConfig{Type: "stdout"})
		case "file":
			sinkConfigs = append(sinkConfigs, SinkConfig{
				Type:     "file",
				Path:     filePath,
				MaxBytes: 100 * 1024 * 1024,
			})
		case "http":
			if httpURL == "" {
				continue
			}
			headers := parseHeaders(httpHeadersStr)
			sinkConfigs = append(sinkConfigs, SinkConfig{
				Type:    "http",
				URL:     httpURL,
				Headers: headers,
			})
		}
	}

	return New(Config{
		Level:   level,
		Service: service,
		Sinks:   sinkConfigs,
	})
}

func parseHeaders(s string) map[string]string {
	headers := make(map[string]string)
	if s == "" {
		return headers
	}
	for _, pair := range strings.Split(s, ",") {
		parts := strings.SplitN(strings.TrimSpace(pair), "=", 2)
		if len(parts) == 2 {
			headers[parts[0]] = parts[1]
		}
	}
	return headers
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
