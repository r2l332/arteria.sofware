package metrics

import (
	"sync"
	"sync/atomic"
	"time"
)

// Counters tracks message throughput and processing statistics.
type Counters struct {
	Received   atomic.Int64
	Processed  atomic.Int64
	Routed     atomic.Int64
	Errors     atomic.Int64
	Rejected   atomic.Int64
	DLQ        atomic.Int64
	BytesIn    atomic.Int64

	startTime time.Time

	// Per-second rate tracking
	mu             sync.Mutex
	rateWindow     []rateSample
	rateWindowSize int

	// Per communication point metrics
	cpMu      sync.RWMutex
	cpMetrics map[string]*CPCounters
}

type rateSample struct {
	ts       time.Time
	received int64
}

// New creates a new Counters instance.
func New() *Counters {
	c := &Counters{
		startTime:      time.Now(),
		rateWindowSize: 60, // 60 seconds of history
		cpMetrics:      make(map[string]*CPCounters),
	}
	go c.sampleLoop()
	return c
}

func (c *Counters) sampleLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		c.mu.Lock()
		c.rateWindow = append(c.rateWindow, rateSample{
			ts:       time.Now(),
			received: c.Received.Load(),
		})
		if len(c.rateWindow) > c.rateWindowSize {
			c.rateWindow = c.rateWindow[1:]
		}
		c.mu.Unlock()
	}
}

// Snapshot returns a point-in-time view of all counters.
type Snapshot struct {
	Received       int64                  `json:"received"`
	Processed      int64                  `json:"processed"`
	Routed         int64                  `json:"routed"`
	Errors         int64                  `json:"errors"`
	Rejected       int64                  `json:"rejected"`
	DLQ            int64                  `json:"dlq"`
	BytesIn        int64                  `json:"bytes_in"`
	UptimeSeconds  float64                `json:"uptime_seconds"`
	MsgsPerSecond  float64                `json:"msgs_per_second"`
	MsgsPerMinute  float64                `json:"msgs_per_minute"`
	CommPoints     map[string]CPSnapshot  `json:"comm_points,omitempty"`
}

// Snap returns the current snapshot.
func (c *Counters) Snap() Snapshot {
	s := Snapshot{
		Received:      c.Received.Load(),
		Processed:     c.Processed.Load(),
		Routed:        c.Routed.Load(),
		Errors:        c.Errors.Load(),
		Rejected:      c.Rejected.Load(),
		DLQ:           c.DLQ.Load(),
		BytesIn:       c.BytesIn.Load(),
		UptimeSeconds: time.Since(c.startTime).Seconds(),
	}

	// Calculate rates
	c.mu.Lock()
	if len(c.rateWindow) >= 2 {
		newest := c.rateWindow[len(c.rateWindow)-1]
		// Per-second: compare last 2 samples
		if len(c.rateWindow) >= 2 {
			prev := c.rateWindow[len(c.rateWindow)-2]
			dt := newest.ts.Sub(prev.ts).Seconds()
			if dt > 0 {
				s.MsgsPerSecond = float64(newest.received-prev.received) / dt
			}
		}
		// Per-minute: compare oldest available to newest
		oldest := c.rateWindow[0]
		dt := newest.ts.Sub(oldest.ts).Seconds()
		if dt > 0 {
			s.MsgsPerMinute = float64(newest.received-oldest.received) / dt * 60
		}
	}
	c.mu.Unlock()

	// Include per-CP metrics
	s.CommPoints = c.SnapCPs()

	return s
}

// --- Per Communication Point Metrics ---

// CPCounters tracks metrics for a single communication point.
type CPCounters struct {
	Name      string
	Direction string // INPUT or OUTPUT
	Received  atomic.Int64
	Sent      atomic.Int64
	Errors    atomic.Int64
	BytesIn   atomic.Int64
	BytesOut  atomic.Int64
	LastSeen  atomic.Int64 // unix timestamp of last message

	// Per-CP log ring buffer
	logMu   sync.Mutex
	logRing []CPLogEntry
	logSize int
	logPos  int
}

// CPLogEntry is a single log entry attached to a communication point.
type CPLogEntry struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Message   string `json:"message"`
	MessageID string `json:"message_id,omitempty"`
	Error     string `json:"error,omitempty"`
	SizeBytes int    `json:"size_bytes,omitempty"`
}

// CPSnapshot is a point-in-time view of a single CP's metrics.
type CPSnapshot struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	Direction string       `json:"direction"`
	Received  int64        `json:"received"`
	Sent      int64        `json:"sent"`
	Errors    int64        `json:"errors"`
	BytesIn   int64        `json:"bytes_in"`
	BytesOut  int64        `json:"bytes_out"`
	LastSeen  string       `json:"last_seen,omitempty"`
	Logs      []CPLogEntry `json:"logs,omitempty"`
}

// ForCP returns (or creates) a CPCounters for the given communication point ID.
func (c *Counters) ForCP(id, name, direction string) *CPCounters {
	c.cpMu.RLock()
	cp, ok := c.cpMetrics[id]
	c.cpMu.RUnlock()
	if ok {
		return cp
	}

	c.cpMu.Lock()
	defer c.cpMu.Unlock()
	// Double check after acquiring write lock
	if cp, ok = c.cpMetrics[id]; ok {
		return cp
	}
	cp = &CPCounters{
		Name:      name,
		Direction: direction,
		logSize:   200, // Keep last 200 log entries per CP
		logRing:   make([]CPLogEntry, 200),
	}
	c.cpMetrics[id] = cp
	return cp
}

// SnapCPs returns snapshots for all tracked communication points.
func (c *Counters) SnapCPs() map[string]CPSnapshot {
	c.cpMu.RLock()
	defer c.cpMu.RUnlock()

	result := make(map[string]CPSnapshot, len(c.cpMetrics))
	for id, cp := range c.cpMetrics {
		snap := CPSnapshot{
			ID:        id,
			Name:      cp.Name,
			Direction: cp.Direction,
			Received:  cp.Received.Load(),
			Sent:      cp.Sent.Load(),
			Errors:    cp.Errors.Load(),
			BytesIn:   cp.BytesIn.Load(),
			BytesOut:  cp.BytesOut.Load(),
			Logs:      cp.GetLogs(50), // Return last 50 entries in snapshot
		}
		if ts := cp.LastSeen.Load(); ts > 0 {
			snap.LastSeen = time.Unix(ts, 0).UTC().Format(time.RFC3339)
		}
		result[id] = snap
	}
	return result
}

// SnapCP returns the snapshot for a single communication point with full logs.
func (c *Counters) SnapCP(id string) (CPSnapshot, bool) {
	c.cpMu.RLock()
	cp, ok := c.cpMetrics[id]
	c.cpMu.RUnlock()
	if !ok {
		return CPSnapshot{}, false
	}
	snap := CPSnapshot{
		ID:        id,
		Name:      cp.Name,
		Direction: cp.Direction,
		Received:  cp.Received.Load(),
		Sent:      cp.Sent.Load(),
		Errors:    cp.Errors.Load(),
		BytesIn:   cp.BytesIn.Load(),
		BytesOut:  cp.BytesOut.Load(),
		Logs:      cp.GetLogs(200), // Full log buffer
	}
	if ts := cp.LastSeen.Load(); ts > 0 {
		snap.LastSeen = time.Unix(ts, 0).UTC().Format(time.RFC3339)
	}
	return snap, true
}

// RecordReceive records a message received on a communication point.
func (cp *CPCounters) RecordReceive(bytes int) {
	cp.Received.Add(1)
	cp.BytesIn.Add(int64(bytes))
	cp.LastSeen.Store(time.Now().Unix())
}

// RecordSend records a message sent from a communication point.
func (cp *CPCounters) RecordSend(bytes int) {
	cp.Sent.Add(1)
	cp.BytesOut.Add(int64(bytes))
	cp.LastSeen.Store(time.Now().Unix())
}

// RecordError records an error on a communication point.
func (cp *CPCounters) RecordError() {
	cp.Errors.Add(1)
}

// Log appends a log entry to the CP's ring buffer.
func (cp *CPCounters) Log(level, message, messageID, errStr string, sizeBytes int) {
	entry := CPLogEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Level:     level,
		Message:   message,
		MessageID: messageID,
		Error:     errStr,
		SizeBytes: sizeBytes,
	}
	cp.logMu.Lock()
	cp.logRing[cp.logPos%cp.logSize] = entry
	cp.logPos++
	cp.logMu.Unlock()
}

// GetLogs returns the last N log entries in chronological order.
func (cp *CPCounters) GetLogs(n int) []CPLogEntry {
	cp.logMu.Lock()
	defer cp.logMu.Unlock()

	total := cp.logPos
	if total == 0 {
		return nil
	}

	count := n
	if total < count {
		count = total
	}
	if count > cp.logSize {
		count = cp.logSize
	}

	result := make([]CPLogEntry, 0, count)
	start := total - count
	for i := start; i < total; i++ {
		entry := cp.logRing[i%cp.logSize]
		if entry.Timestamp != "" {
			result = append(result, entry)
		}
	}
	return result
}
