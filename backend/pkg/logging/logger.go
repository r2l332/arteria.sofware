package logging

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Level represents log verbosity.
type Level int

const (
	LevelTrace Level = iota
	LevelDebug
	LevelInfo
	LevelWarn
	LevelError
	LevelFatal
)

var levelNames = map[Level]string{
	LevelTrace: "TRACE",
	LevelDebug: "DEBUG",
	LevelInfo:  "INFO",
	LevelWarn:  "WARN",
	LevelError: "ERROR",
	LevelFatal: "FATAL",
}

// String returns the level name.
func (l Level) String() string {
	if name, ok := levelNames[l]; ok {
		return name
	}
	return "UNKNOWN"
}

// ParseLevel converts a string level name to Level.
func ParseLevel(s string) Level {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "TRACE":
		return LevelTrace
	case "DEBUG":
		return LevelDebug
	case "INFO":
		return LevelInfo
	case "WARN", "WARNING":
		return LevelWarn
	case "ERROR":
		return LevelError
	case "FATAL":
		return LevelFatal
	default:
		return LevelInfo
	}
}

// Entry is a structured log entry.
type Entry struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Service   string `json:"service"`
	Component string `json:"component,omitempty"`
	Message   string `json:"message"`
	Fields    Fields `json:"fields,omitempty"`
}

// Fields holds arbitrary structured data attached to a log entry.
type Fields map[string]interface{}

// Sink defines where logs are shipped to.
type Sink interface {
	Write(entry Entry) error
	Close() error
}

// --- Sink implementations ---

// StdoutSink writes JSON logs to stdout.
type StdoutSink struct {
	mu  sync.Mutex
	enc *json.Encoder
}

func NewStdoutSink() *StdoutSink {
	return &StdoutSink{enc: json.NewEncoder(os.Stdout)}
}

func (s *StdoutSink) Write(entry Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enc.Encode(entry)
}

func (s *StdoutSink) Close() error { return nil }

// FileSink writes JSON logs to a file with rotation support.
type FileSink struct {
	mu       sync.Mutex
	file     *os.File
	path     string
	maxBytes int64
	written  int64
}

func NewFileSink(path string, maxBytes int64) (*FileSink, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("open log file %s: %w", path, err)
	}
	info, _ := f.Stat()
	return &FileSink{file: f, path: path, maxBytes: maxBytes, written: info.Size()}, nil
}

func (s *FileSink) Write(entry Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	// Rotate if needed
	if s.maxBytes > 0 && s.written+int64(len(data)) > s.maxBytes {
		s.file.Close()
		rotated := fmt.Sprintf("%s.%d", s.path, time.Now().Unix())
		os.Rename(s.path, rotated)
		f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}
		s.file = f
		s.written = 0
	}

	n, err := s.file.Write(data)
	s.written += int64(n)
	return err
}

func (s *FileSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.file.Close()
}

// HTTPSink ships logs to an external HTTP endpoint (e.g., Loki, Splunk HEC, Logstash).
type HTTPSink struct {
	url     string
	headers map[string]string
	client  *http.Client
	batch   []Entry
	mu      sync.Mutex
	maxBuf  int
	flushCh chan struct{}
	done    chan struct{}
}

func NewHTTPSink(url string, headers map[string]string, batchSize int, flushInterval time.Duration) *HTTPSink {
	s := &HTTPSink{
		url:     url,
		headers: headers,
		client:  &http.Client{Timeout: 5 * time.Second},
		batch:   make([]Entry, 0, batchSize),
		maxBuf:  batchSize,
		flushCh: make(chan struct{}, 1),
		done:    make(chan struct{}),
	}
	go s.flusher(flushInterval)
	return s
}

func (s *HTTPSink) Write(entry Entry) error {
	s.mu.Lock()
	s.batch = append(s.batch, entry)
	shouldFlush := len(s.batch) >= s.maxBuf
	s.mu.Unlock()

	if shouldFlush {
		select {
		case s.flushCh <- struct{}{}:
		default:
		}
	}
	return nil
}

func (s *HTTPSink) flusher(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.flush()
		case <-s.flushCh:
			s.flush()
		case <-s.done:
			s.flush()
			return
		}
	}
}

func (s *HTTPSink) flush() {
	s.mu.Lock()
	if len(s.batch) == 0 {
		s.mu.Unlock()
		return
	}
	toSend := s.batch
	s.batch = make([]Entry, 0, s.maxBuf)
	s.mu.Unlock()

	data, err := json.Marshal(toSend)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[LOG] marshal error: %v\n", err)
		return
	}

	req, err := http.NewRequest("POST", s.url, strings.NewReader(string(data)))
	if err != nil {
		fmt.Fprintf(os.Stderr, "[LOG] request error: %v\n", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range s.headers {
		req.Header.Set(k, v)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[LOG] ship error: %v\n", err)
		return
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()
}

func (s *HTTPSink) Close() error {
	close(s.done)
	return nil
}

// --- Logger ---

// Logger is the main structured logger with configurable level and multiple sinks.
type Logger struct {
	level     Level
	service   string
	component string
	sinks     []Sink
	mu        sync.RWMutex
}

// Config holds logger configuration.
type Config struct {
	Level     string // TRACE, DEBUG, INFO, WARN, ERROR, FATAL
	Service   string
	Sinks     []SinkConfig
}

// SinkConfig defines a log destination.
type SinkConfig struct {
	Type     string            // "stdout", "file", "http"
	Path     string            // For file sink
	MaxBytes int64             // For file sink rotation (0 = no rotation)
	URL      string            // For HTTP sink
	Headers  map[string]string // For HTTP sink
}

// New creates a logger from config.
func New(cfg Config) (*Logger, error) {
	l := &Logger{
		level:   ParseLevel(cfg.Level),
		service: cfg.Service,
	}

	for _, sc := range cfg.Sinks {
		switch sc.Type {
		case "stdout":
			l.sinks = append(l.sinks, NewStdoutSink())
		case "file":
			maxBytes := sc.MaxBytes
			if maxBytes == 0 {
				maxBytes = 100 * 1024 * 1024 // 100MB default
			}
			sink, err := NewFileSink(sc.Path, maxBytes)
			if err != nil {
				return nil, err
			}
			l.sinks = append(l.sinks, sink)
		case "http":
			l.sinks = append(l.sinks, NewHTTPSink(sc.URL, sc.Headers, 100, 5*time.Second))
		default:
			return nil, fmt.Errorf("unknown sink type: %s", sc.Type)
		}
	}

	if len(l.sinks) == 0 {
		l.sinks = append(l.sinks, NewStdoutSink())
	}

	return l, nil
}

// WithComponent returns a sub-logger scoped to a component.
func (l *Logger) WithComponent(name string) *Logger {
	return &Logger{
		level:     l.level,
		service:   l.service,
		component: name,
		sinks:     l.sinks,
	}
}

// SetLevel changes the log level at runtime.
func (l *Logger) SetLevel(level Level) {
	l.mu.Lock()
	l.level = level
	l.mu.Unlock()
}

// GetLevel returns the current level.
func (l *Logger) GetLevel() Level {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.level
}

func (l *Logger) log(level Level, msg string, fields Fields) {
	l.mu.RLock()
	currentLevel := l.level
	l.mu.RUnlock()

	if level < currentLevel {
		return
	}

	entry := Entry{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Level:     levelNames[level],
		Service:   l.service,
		Component: l.component,
		Message:   msg,
		Fields:    fields,
	}

	for _, sink := range l.sinks {
		sink.Write(entry)
	}

	if level == LevelFatal {
		os.Exit(1)
	}
}

func (l *Logger) Trace(msg string, fields ...Fields) { l.log(LevelTrace, msg, mergeFields(fields)) }
func (l *Logger) Debug(msg string, fields ...Fields) { l.log(LevelDebug, msg, mergeFields(fields)) }
func (l *Logger) Info(msg string, fields ...Fields)  { l.log(LevelInfo, msg, mergeFields(fields)) }
func (l *Logger) Warn(msg string, fields ...Fields)  { l.log(LevelWarn, msg, mergeFields(fields)) }
func (l *Logger) Error(msg string, fields ...Fields) { l.log(LevelError, msg, mergeFields(fields)) }
func (l *Logger) Fatal(msg string, fields ...Fields) { l.log(LevelFatal, msg, mergeFields(fields)) }

// Close flushes and closes all sinks.
func (l *Logger) Close() {
	for _, sink := range l.sinks {
		sink.Close()
	}
}

func mergeFields(ff []Fields) Fields {
	if len(ff) == 0 {
		return nil
	}
	result := Fields{}
	for _, f := range ff {
		for k, v := range f {
			result[k] = v
		}
	}
	return result
}
