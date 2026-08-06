package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	startBlock = 0x0B
	endBlock   = 0x1C
	cr         = 0x0D
)

// --- Random HL7 Generator ---

var firstNames = []string{
	"James", "Mary", "Robert", "Patricia", "John", "Jennifer", "Michael", "Linda",
	"David", "Elizabeth", "William", "Barbara", "Richard", "Susan", "Joseph", "Jessica",
	"Thomas", "Sarah", "Charles", "Karen", "Daniel", "Lisa", "Matthew", "Nancy",
	"Anthony", "Betty", "Mark", "Margaret", "Donald", "Sandra", "Steven", "Ashley",
}

var lastNames = []string{
	"Smith", "Johnson", "Williams", "Brown", "Jones", "Garcia", "Miller", "Davis",
	"Rodriguez", "Martinez", "Hernandez", "Lopez", "Gonzalez", "Wilson", "Anderson",
	"Thomas", "Taylor", "Moore", "Jackson", "Martin", "Lee", "Perez", "Thompson",
	"White", "Harris", "Sanchez", "Clark", "Ramirez", "Lewis", "Robinson", "Walker",
}

var facilities = []string{
	"GENERAL_HOSP", "CITY_MED", "COUNTY_CLINIC", "ST_JAMES", "MEMORIAL",
}

func randomPatientID() string {
	return fmt.Sprintf("MRN%05d", rand.Intn(99999)+1)
}

func randomName() (first, last string) {
	return firstNames[rand.Intn(len(firstNames))], lastNames[rand.Intn(len(lastNames))]
}

func generateHL7(msgID int) string {
	first, last := randomName()
	pid := randomPatientID()
	facility := facilities[rand.Intn(len(facilities))]
	ts := time.Now().Format("20060102150405")
	dob := fmt.Sprintf("%d%02d%02d", 1950+rand.Intn(50), rand.Intn(12)+1, rand.Intn(28)+1)
	sex := "M"
	if rand.Intn(2) == 0 {
		sex = "F"
	}

	return fmt.Sprintf(
		"MSH|^~\\&|EMR|%s|ARTERIA|CLOUD|%s||ADT^A04|MSG%06d|P|2.3\r"+
			"PID|||%s||%s^%s||%s|%s|||123 Main St^^Springfield^IL^62701\r"+
			"PV1||I|ICU^01^01||||1234^Smith^Robert|||SUR||||ADM|A0\r",
		facility, ts, msgID, pid, last, first, dob, sex,
	)
}

// --- MLLP Sender ---

func mllpSend(addr string, hl7 string) error {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("connect %s: %w", addr, err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(10 * time.Second))

	var buf bytes.Buffer
	buf.WriteByte(startBlock)
	buf.WriteString(hl7)
	buf.WriteByte(endBlock)
	buf.WriteByte(cr)

	if _, err := conn.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("write: %w", err)
	}

	// Read ACK (optional)
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	ack := make([]byte, 1024)
	conn.Read(ack) // Best-effort ACK read
	return nil
}

// --- MLLP Receiver (gets processed results back) ---

type ProcessedMessage struct {
	Timestamp  time.Time `json:"timestamp"`
	RawPayload string    `json:"raw_payload"`
	Size       int       `json:"size"`
	From       string    `json:"from"`
}

type Receiver struct {
	listener net.Listener
	messages []ProcessedMessage
	mu       sync.Mutex
	count    atomic.Int64
	quit     chan struct{}
	wg       sync.WaitGroup
}

func newReceiver(addr string) (*Receiver, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	r := &Receiver{listener: ln, quit: make(chan struct{})}
	go r.acceptLoop()
	return r, nil
}

func (r *Receiver) acceptLoop() {
	for {
		conn, err := r.listener.Accept()
		if err != nil {
			select {
			case <-r.quit:
				return
			default:
				continue
			}
		}
		r.wg.Add(1)
		go r.handleConn(conn)
	}
}

func (r *Receiver) handleConn(conn net.Conn) {
	defer r.wg.Done()
	defer conn.Close()
	reader := bufio.NewReader(conn)

	for {
		msg, err := readMLLPFrame(reader)
		if err != nil {
			return
		}
		if len(msg) == 0 {
			continue
		}
		r.mu.Lock()
		r.messages = append(r.messages, ProcessedMessage{
			Timestamp:  time.Now(),
			RawPayload: string(msg),
			Size:       len(msg),
			From:       conn.RemoteAddr().String(),
		})
		r.mu.Unlock()
		r.count.Add(1)
		log.Printf("[RECEIVER] got processed message #%d (%d bytes)", r.count.Load(), len(msg))
	}
}

func (r *Receiver) getMessages() []ProcessedMessage {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]ProcessedMessage, len(r.messages))
	copy(result, r.messages)
	return result
}

func readMLLPFrame(r *bufio.Reader) ([]byte, error) {
	// Wait for start block
	for {
		b, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		if b == startBlock {
			break
		}
	}
	var buf bytes.Buffer
	for {
		b, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		if b == endBlock {
			next, err := r.Peek(1)
			if err == nil && len(next) > 0 && next[0] == cr {
				r.ReadByte()
			}
			break
		}
		buf.WriteByte(b)
	}
	return buf.Bytes(), nil
}

// --- Web UI for viewing processed messages ---

type Stats struct {
	Sent       int64   `json:"sent"`
	Received   int64   `json:"received"`
	Errors     int64   `json:"errors"`
	LastError  string  `json:"last_error,omitempty"`
	UptimeSec  float64 `json:"uptime_sec"`
	AvgLatency float64 `json:"avg_latency_ms"`
}

var (
	sentCount  atomic.Int64
	errCount   atomic.Int64
	lastError  atomic.Value // string
	startTime  time.Time
	latencies  []float64
	latencyMu  sync.Mutex
)

func recordLatency(d time.Duration) {
	latencyMu.Lock()
	latencies = append(latencies, float64(d.Milliseconds()))
	if len(latencies) > 1000 {
		latencies = latencies[len(latencies)-1000:]
	}
	latencyMu.Unlock()
}

func avgLatency() float64 {
	latencyMu.Lock()
	defer latencyMu.Unlock()
	if len(latencies) == 0 {
		return 0
	}
	var sum float64
	for _, l := range latencies {
		sum += l
	}
	return sum / float64(len(latencies))
}

func startWebUI(receiver *Receiver, port string) {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(dashboardHTML))
	})

	mux.HandleFunc("/api/stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		le, _ := lastError.Load().(string)
		json.NewEncoder(w).Encode(Stats{
			Sent:       sentCount.Load(),
			Received:   receiver.count.Load(),
			Errors:     errCount.Load(),
			LastError:  le,
			UptimeSec:  time.Since(startTime).Seconds(),
			AvgLatency: avgLatency(),
		})
	})

	mux.HandleFunc("/api/messages", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		msgs := receiver.getMessages()
		// Return last 100
		if len(msgs) > 100 {
			msgs = msgs[len(msgs)-100:]
		}
		json.NewEncoder(w).Encode(msgs)
	})

	mux.HandleFunc("/api/sent", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]int64{"count": sentCount.Load()})
	})

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	log.Printf("[WEB] Dashboard at http://localhost:%s", port)
	srv := &http.Server{Addr: ":" + port, Handler: mux}
	go srv.ListenAndServe()
}

// --- Main ---

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	startTime = time.Now()

	if len(os.Args) < 2 {
		runGeneratorMode()
		return
	}

	switch os.Args[1] {
	case "generate", "run":
		runGeneratorMode()
	case "receive":
		runReceiverOnly()
	case "send":
		runSendOnly()
	case "test":
		runTests()
	case "send-one":
		runSendOne()
	default:
		fmt.Println("Arteria Capillary E2E Demo — VM Application")
		fmt.Println()
		fmt.Println("Usage: vm-app <command>")
		fmt.Println()
		fmt.Println("Commands:")
		fmt.Println("  run          Full mode: generate HL7, receive results, serve dashboard (default)")
		fmt.Println("  receive      Receiver-only: listen for processed results + dashboard")
		fmt.Println("  send         Sender-only: generate and send HL7 messages")
		fmt.Println("  send-one     Send a single HL7 message and exit")
		fmt.Println("  test         Run E2E verification tests")
		fmt.Println()
		fmt.Println("Environment:")
		fmt.Println("  SEND_ADDR          MLLP target (default: localhost:2575)")
		fmt.Println("  RECV_ADDR          Listen address for results (default: :2576)")
		fmt.Println("  WEB_PORT           Dashboard port (default: 8090)")
		fmt.Println("  SEND_INTERVAL_MS   Delay between sends (default: 3000)")
		fmt.Println("  BURST_SIZE         Messages per interval (default: 1)")
		fmt.Println("  SEND_COUNT         Total messages to send, 0=infinite (default: 0)")
		fmt.Println("  CONNECT_TIMEOUT_S  Seconds to wait for MLLP target (default: 30)")
		os.Exit(0)
	}
}

func runGeneratorMode() {
	sendAddr := envOr("SEND_ADDR", "localhost:2575")
	recvAddr := envOr("RECV_ADDR", ":2576")
	webPort := envOr("WEB_PORT", "8090")
	intervalMs := envOrInt("SEND_INTERVAL_MS", 3000)
	burstSize := envOrInt("BURST_SIZE", 1)
	sendCount := envOrInt("SEND_COUNT", 0)
	connectTimeout := envOrInt("CONNECT_TIMEOUT_S", 30)

	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║   Arteria Capillary E2E Demo — VM Application               ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════╣")
	fmt.Printf("║  Mode:              %-40s ║\n", "generate + receive")
	fmt.Printf("║  Sending HL7 to:    %-40s ║\n", sendAddr)
	fmt.Printf("║  Receiving on:      %-40s ║\n", recvAddr)
	fmt.Printf("║  Dashboard:         http://localhost:%-23s ║\n", webPort)
	fmt.Printf("║  Interval:          %dms (burst: %d)%-26s║\n", intervalMs, burstSize, "")
	if sendCount > 0 {
		fmt.Printf("║  Send count:        %-40d ║\n", sendCount)
	} else {
		fmt.Printf("║  Send count:        %-40s ║\n", "infinite")
	}
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")

	// Start receiver
	receiver, err := newReceiver(recvAddr)
	if err != nil {
		log.Fatalf("Failed to start receiver on %s: %v", recvAddr, err)
	}
	log.Printf("[RECEIVER] Listening for processed results on %s", recvAddr)

	// Start dashboard
	startWebUI(receiver, webPort)

	// Wait for send target to be reachable
	waitForTarget(sendAddr, time.Duration(connectTimeout)*time.Second)

	// Handle graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Start sending
	msgID := 1
	ticker := time.NewTicker(time.Duration(intervalMs) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-sigCh:
			fmt.Printf("\nShutdown. Sent: %d, Received: %d, Errors: %d\n",
				sentCount.Load(), receiver.count.Load(), errCount.Load())
			return
		case <-ticker.C:
			for i := 0; i < burstSize; i++ {
				hl7 := generateHL7(msgID)
				sendStart := time.Now()
				if err := mllpSend(sendAddr, hl7); err != nil {
					errCount.Add(1)
					lastError.Store(err.Error())
					log.Printf("[SENDER] Error MSG%06d: %v", msgID, err)
				} else {
					recordLatency(time.Since(sendStart))
					sentCount.Add(1)
					log.Printf("[SENDER] Sent MSG%06d (%d bytes)", msgID, len(hl7))
				}
				msgID++
				if sendCount > 0 && msgID > sendCount {
					log.Printf("[SENDER] Reached send count %d. Waiting for results...", sendCount)
					// Keep running for dashboard/receiver
					<-sigCh
					fmt.Printf("\nDone. Sent: %d, Received: %d, Errors: %d\n",
						sentCount.Load(), receiver.count.Load(), errCount.Load())
					return
				}
			}
		}
	}
}

func runReceiverOnly() {
	recvAddr := envOr("RECV_ADDR", ":2576")
	webPort := envOr("WEB_PORT", "8090")

	fmt.Printf("[RECEIVER] Listening on %s, dashboard on :%s\n", recvAddr, webPort)

	receiver, err := newReceiver(recvAddr)
	if err != nil {
		log.Fatalf("Failed to start receiver: %v", err)
	}

	startWebUI(receiver, webPort)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	fmt.Printf("\nReceived %d messages total.\n", receiver.count.Load())
}

func runSendOnly() {
	sendAddr := envOr("SEND_ADDR", "localhost:2575")
	intervalMs := envOrInt("SEND_INTERVAL_MS", 3000)
	burstSize := envOrInt("BURST_SIZE", 1)
	sendCount := envOrInt("SEND_COUNT", 0)
	connectTimeout := envOrInt("CONNECT_TIMEOUT_S", 30)

	fmt.Printf("[SENDER] Sending to %s every %dms (burst=%d)\n", sendAddr, intervalMs, burstSize)

	waitForTarget(sendAddr, time.Duration(connectTimeout)*time.Second)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	msgID := 1
	ticker := time.NewTicker(time.Duration(intervalMs) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-sigCh:
			fmt.Printf("\nSent %d messages, %d errors.\n", sentCount.Load(), errCount.Load())
			return
		case <-ticker.C:
			for i := 0; i < burstSize; i++ {
				hl7 := generateHL7(msgID)
				if err := mllpSend(sendAddr, hl7); err != nil {
					errCount.Add(1)
					log.Printf("[SENDER] Error: %v", err)
				} else {
					sentCount.Add(1)
					log.Printf("[SENDER] Sent MSG%06d", msgID)
				}
				msgID++
				if sendCount > 0 && msgID > sendCount {
					fmt.Printf("Done. Sent %d messages.\n", sentCount.Load())
					return
				}
			}
		}
	}
}

func runSendOne() {
	sendAddr := envOr("SEND_ADDR", "localhost:2575")
	hl7 := generateHL7(1)
	fmt.Printf("Sending single HL7 to %s...\n", sendAddr)
	if err := mllpSend(sendAddr, hl7); err != nil {
		log.Fatalf("Send failed: %v", err)
	}
	fmt.Println("Sent OK.")
	fmt.Printf("Message:\n%s\n", strings.ReplaceAll(hl7, "\r", "\n"))
}

// --- E2E Test Suite ---

func runTests() {
	sendAddr := envOr("SEND_ADDR", "localhost:2575")
	recvAddr := envOr("RECV_ADDR", ":2576")
	connectTimeout := envOrInt("CONNECT_TIMEOUT_S", 30)
	testCount := envOrInt("TEST_COUNT", 5)

	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║   Arteria Capillary E2E Test Suite                           ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════╣")
	fmt.Printf("║  Send to:     %-45s ║\n", sendAddr)
	fmt.Printf("║  Receive on:  %-45s ║\n", recvAddr)
	fmt.Printf("║  Test count:  %-45d ║\n", testCount)
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Println()

	suite := &TestSuite{Name: "Capillary E2E"}

	// Test 1: Receiver can bind
	var receiver *Receiver
	suite.run("Receiver binds to port", func() error {
		var err error
		receiver, err = newReceiver(recvAddr)
		return err
	})
	if receiver == nil {
		fmt.Println("\nFATAL: Cannot start receiver, aborting tests.")
		os.Exit(1)
	}

	// Test 2: Target is reachable
	suite.run("MLLP target reachable", func() error {
		return waitForTargetErr(sendAddr, time.Duration(connectTimeout)*time.Second)
	})

	// Test 3: Send single message
	suite.run("Send single HL7 message", func() error {
		hl7 := generateHL7(9001)
		return mllpSend(sendAddr, hl7)
	})

	// Test 4: Send batch and verify receipt
	suite.run(fmt.Sprintf("Send %d messages and receive results", testCount), func() error {
		beforeRecv := receiver.count.Load()
		for i := 0; i < testCount; i++ {
			hl7 := generateHL7(9100 + i)
			if err := mllpSend(sendAddr, hl7); err != nil {
				return fmt.Errorf("send msg %d: %w", i, err)
			}
			time.Sleep(200 * time.Millisecond)
		}
		// Wait for results to come back (up to 30s)
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			received := receiver.count.Load() - beforeRecv
			if received >= int64(testCount) {
				return nil
			}
			time.Sleep(500 * time.Millisecond)
		}
		received := receiver.count.Load() - beforeRecv
		if received == 0 {
			return fmt.Errorf("received 0/%d messages (timeout 30s)", testCount)
		}
		return fmt.Errorf("received %d/%d messages (partial)", received, testCount)
	})

	// Test 5: Verify message content is valid JSON
	suite.run("Received messages contain valid JSON", func() error {
		msgs := receiver.getMessages()
		if len(msgs) == 0 {
			return fmt.Errorf("no messages received")
		}
		for i, msg := range msgs {
			if !json.Valid([]byte(msg.RawPayload)) {
				return fmt.Errorf("message %d is not valid JSON: %s", i, msg.RawPayload[:min(100, len(msg.RawPayload))])
			}
		}
		return nil
	})

	// Test 6: Verify patient data extraction
	suite.run("Patient ID and name present in output", func() error {
		msgs := receiver.getMessages()
		if len(msgs) == 0 {
			return fmt.Errorf("no messages to verify")
		}
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(msgs[len(msgs)-1].RawPayload), &parsed); err != nil {
			return fmt.Errorf("parse last message: %w", err)
		}
		if _, ok := parsed["patient_id"]; !ok {
			return fmt.Errorf("missing patient_id in: %v", parsed)
		}
		if _, ok := parsed["patient_name"]; !ok {
			return fmt.Errorf("missing patient_name in: %v", parsed)
		}
		pid := fmt.Sprintf("%v", parsed["patient_id"])
		name := fmt.Sprintf("%v", parsed["patient_name"])
		if !strings.HasPrefix(pid, "MRN") {
			return fmt.Errorf("patient_id doesn't look like MRN*: %q", pid)
		}
		if name == "" {
			return fmt.Errorf("patient_name is empty")
		}
		fmt.Printf("    → Verified patient: %s (%s)\n", name, pid)
		return nil
	})

	// Test 7: Throughput test
	suite.run("Throughput: 20 messages in rapid succession", func() error {
		beforeRecv := receiver.count.Load()
		start := time.Now()
		for i := 0; i < 20; i++ {
			hl7 := generateHL7(9500 + i)
			if err := mllpSend(sendAddr, hl7); err != nil {
				return fmt.Errorf("send %d: %w", i, err)
			}
		}
		elapsed := time.Since(start)
		// Wait up to 60s for all to come back
		deadline := time.Now().Add(60 * time.Second)
		for time.Now().Before(deadline) {
			if receiver.count.Load()-beforeRecv >= 20 {
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
		received := receiver.count.Load() - beforeRecv
		roundTrip := time.Since(start)
		fmt.Printf("    → Sent 20 in %v, received %d back in %v\n", elapsed, received, roundTrip)
		if received < 15 {
			return fmt.Errorf("only %d/20 received back", received)
		}
		return nil
	})

	// Summary
	suite.summary()

	if suite.Failed > 0 {
		os.Exit(1)
	}
}

// --- Test infrastructure ---

type TestResult struct {
	Name     string  `json:"name"`
	Passed   bool    `json:"passed"`
	Duration float64 `json:"duration_ms"`
	Error    string  `json:"error,omitempty"`
}

type TestSuite struct {
	Name    string
	Results []TestResult
	Passed  int
	Failed  int
}

func (s *TestSuite) run(name string, fn func() error) {
	start := time.Now()
	err := fn()
	dur := time.Since(start).Seconds() * 1000

	r := TestResult{Name: name, Duration: dur}
	if err != nil {
		r.Passed = false
		r.Error = err.Error()
		s.Failed++
		fmt.Printf("  ✗ %s (%.0fms) — %v\n", name, dur, err)
	} else {
		r.Passed = true
		s.Passed++
		fmt.Printf("  ✓ %s (%.0fms)\n", name, dur)
	}
	s.Results = append(s.Results, r)
}

func (s *TestSuite) summary() {
	total := s.Passed + s.Failed
	status := "PASS"
	if s.Failed > 0 {
		status = "FAIL"
	}
	fmt.Printf("\n══════════════════════════════════════════\n")
	fmt.Printf("[%s] %s: %d/%d passed\n", status, s.Name, s.Passed, total)
	fmt.Printf("══════════════════════════════════════════\n")
}

// --- Helpers ---

func waitForTarget(addr string, timeout time.Duration) {
	if err := waitForTargetErr(addr, timeout); err != nil {
		log.Printf("[SENDER] Warning: %v — will retry on send", err)
	}
}

func waitForTargetErr(addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	attempt := 0
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err == nil {
			conn.Close()
			if attempt > 0 {
				log.Printf("[SENDER] Target %s reachable after %d attempts", addr, attempt)
			}
			return nil
		}
		attempt++
		if attempt%5 == 0 {
			log.Printf("[SENDER] Waiting for %s... (attempt %d)", addr, attempt)
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("target %s not reachable after %v", addr, timeout)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envOrInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	var i int
	fmt.Sscanf(v, "%d", &i)
	if i == 0 {
		return def
	}
	return i
}

const dashboardHTML = `<!DOCTYPE html>
<html>
<head>
<title>Arteria Capillary Demo — VM Dashboard</title>
<style>
* { margin: 0; padding: 0; box-sizing: border-box; }
body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif; background: #0f172a; color: #e2e8f0; padding: 20px; }
h1 { color: #38bdf8; margin-bottom: 20px; }
.stats { display: flex; gap: 16px; margin-bottom: 20px; flex-wrap: wrap; }
.stat-card { background: #1e293b; border: 1px solid #334155; border-radius: 8px; padding: 16px 20px; min-width: 150px; flex: 1; }
.stat-card h3 { color: #94a3b8; font-size: 0.75rem; text-transform: uppercase; letter-spacing: 0.05em; }
.stat-card .value { font-size: 1.75rem; font-weight: bold; color: #38bdf8; margin-top: 4px; }
.stat-card .value.received { color: #4ade80; }
.stat-card .value.errors { color: #f87171; }
.stat-card .value.latency { color: #fbbf24; }
.messages { background: #1e293b; border: 1px solid #334155; border-radius: 8px; padding: 20px; max-height: 600px; overflow-y: auto; }
.messages h2 { color: #94a3b8; margin-bottom: 12px; font-size: 0.9rem; text-transform: uppercase; }
.msg { background: #0f172a; border: 1px solid #334155; border-radius: 4px; padding: 12px; margin-bottom: 8px; font-family: 'SF Mono', 'Fira Code', monospace; font-size: 0.8rem; }
.msg .ts { color: #64748b; font-size: 0.7rem; margin-bottom: 4px; }
.msg .payload { color: #4ade80; white-space: pre-wrap; word-break: break-all; }
.flow { background: #1e293b; border: 1px solid #334155; border-radius: 8px; padding: 20px; margin-bottom: 20px; }
.flow h2 { color: #94a3b8; margin-bottom: 12px; font-size: 0.9rem; text-transform: uppercase; }
.flow-diagram { font-family: monospace; color: #38bdf8; font-size: 0.85rem; text-align: center; padding: 10px; line-height: 1.8; }
.flow-diagram .arrow { color: #64748b; }
.error-banner { background: #7f1d1d; border: 1px solid #991b1b; border-radius: 8px; padding: 12px 16px; margin-bottom: 16px; color: #fca5a5; font-size: 0.85rem; display: none; }
</style>
</head>
<body>
<h1>Arteria Capillary E2E Demo</h1>

<div id="error-banner" class="error-banner"></div>

<div class="flow">
<h2>Message Flow</h2>
<div class="flow-diagram">
VM (HL7 Generator) <span class="arrow">→</span> MLLP :2575 <span class="arrow">→</span> Capillary Agent <span class="arrow">→</span> mTLS Tunnel <span class="arrow">→</span> Broker <span class="arrow">→</span> Ingestion<br>
<span class="arrow">→</span> Processing Engine <span class="arrow">→</span> Python: HL7→JSON <span class="arrow">→</span> Python: Extract Patient<br>
<span class="arrow">→</span> Egress <span class="arrow">→</span> Broker <span class="arrow">→</span> mTLS Tunnel <span class="arrow">→</span> Capillary Agent <span class="arrow">→</span> MLLP :2576 <span class="arrow">→</span> This Dashboard
</div>
</div>

<div class="stats">
<div class="stat-card"><h3>Sent</h3><div class="value" id="sent">0</div></div>
<div class="stat-card"><h3>Received</h3><div class="value received" id="received">0</div></div>
<div class="stat-card"><h3>Errors</h3><div class="value errors" id="errors">0</div></div>
<div class="stat-card"><h3>Avg Latency</h3><div class="value latency" id="latency">-</div></div>
<div class="stat-card"><h3>Uptime</h3><div class="value" id="uptime">0s</div></div>
</div>

<div class="messages">
<h2>Processed Patient Extracts (most recent first)</h2>
<div id="msg-list"><p style="color:#64748b">Waiting for messages...</p></div>
</div>

<script>
async function refresh() {
  try {
    const stats = await fetch('/api/stats').then(r => r.json());
    document.getElementById('sent').textContent = stats.sent;
    document.getElementById('received').textContent = stats.received;
    document.getElementById('errors').textContent = stats.errors;
    document.getElementById('latency').textContent = stats.avg_latency_ms > 0 ? stats.avg_latency_ms.toFixed(0) + 'ms' : '-';
    const mins = Math.floor(stats.uptime_sec / 60);
    const secs = Math.floor(stats.uptime_sec % 60);
    document.getElementById('uptime').textContent = mins > 0 ? mins + 'm ' + secs + 's' : secs + 's';

    const errBanner = document.getElementById('error-banner');
    if (stats.last_error) {
      errBanner.textContent = 'Last error: ' + stats.last_error;
      errBanner.style.display = 'block';
    } else {
      errBanner.style.display = 'none';
    }

    const msgs = await fetch('/api/messages').then(r => r.json());
    const list = document.getElementById('msg-list');
    if (!msgs || msgs.length === 0) return;
    list.innerHTML = '';
    for (const m of (msgs || []).reverse()) {
      const div = document.createElement('div');
      div.className = 'msg';
      const ts = new Date(m.timestamp).toLocaleTimeString();
      let payload = m.raw_payload;
      try { payload = JSON.stringify(JSON.parse(payload), null, 2); } catch(e) {}
      div.innerHTML = '<div class="ts">' + ts + ' | ' + m.size + ' bytes | from ' + escapeHtml(m.from) + '</div><div class="payload">' + escapeHtml(payload) + '</div>';
      list.appendChild(div);
    }
  } catch(e) { console.error(e); }
}

function escapeHtml(s) {
  return s.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
}

setInterval(refresh, 2000);
refresh();
</script>
</body>
</html>`
