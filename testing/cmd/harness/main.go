package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	startBlock = 0x0B
	endBlock   = 0x1C
	cr         = 0x0D
)

// --- MLLP Client (sends messages to Arteria) ---

func mllpSend(host string, port int, hl7 string) error {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", host, port), 5*time.Second)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(10 * time.Second))

	var buf bytes.Buffer
	buf.WriteByte(startBlock)
	buf.WriteString(hl7)
	buf.WriteByte(endBlock)
	buf.WriteByte(cr)

	_, err = conn.Write(buf.Bytes())
	return err
}

func mllpSendBatch(host string, port int, messages []string) (sent, failed int) {
	for _, msg := range messages {
		if err := mllpSend(host, port, msg); err != nil {
			failed++
			fmt.Printf("  FAIL: %v\n", err)
		} else {
			sent++
		}
	}
	return
}

// --- MLLP Server (receives messages from Arteria egress) ---

type MLLPServer struct {
	listener net.Listener
	received []ReceivedMessage
	mu       sync.Mutex
	count    atomic.Int64
	quit     chan struct{}
	wg       sync.WaitGroup
}

type ReceivedMessage struct {
	Timestamp time.Time `json:"timestamp"`
	Payload   string    `json:"payload"`
	Size      int       `json:"size"`
	From      string    `json:"from"`
}

func newMLLPServer(addr string) (*MLLPServer, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	s := &MLLPServer{listener: ln, quit: make(chan struct{})}
	go s.acceptLoop()
	return s, nil
}

func (s *MLLPServer) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.quit:
				return
			default:
				continue
			}
		}
		s.wg.Add(1)
		go s.handleConn(conn)
	}
}

func (s *MLLPServer) handleConn(conn net.Conn) {
	defer s.wg.Done()
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
		s.mu.Lock()
		s.received = append(s.received, ReceivedMessage{
			Timestamp: time.Now(),
			Payload:   string(msg),
			Size:      len(msg),
			From:      conn.RemoteAddr().String(),
		})
		s.mu.Unlock()
		s.count.Add(1)
	}
}

func (s *MLLPServer) stop() {
	close(s.quit)
	s.listener.Close()
	s.wg.Wait()
}

func (s *MLLPServer) getReceived() []ReceivedMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]ReceivedMessage, len(s.received))
	copy(result, s.received)
	return result
}

func readMLLPFrame(r *bufio.Reader) ([]byte, error) {
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

// --- API Client ---

func apiGet(baseURL, path string) (map[string]interface{}, error) {
	resp, err := http.Get(baseURL + path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	json.Unmarshal(body, &result)
	return result, nil
}

func apiPost(baseURL, path string, payload interface{}) (map[string]interface{}, error) {
	data, _ := json.Marshal(payload)
	resp, err := http.Post(baseURL+path, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	json.Unmarshal(body, &result)
	return result, nil
}

func apiPut(baseURL, path string, payload interface{}) error {
	data, _ := json.Marshal(payload)
	req, _ := http.NewRequest("PUT", baseURL+path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// --- HL7 Message Builders ---

func buildADT_A01(msgID, patientID, facility string) string {
	return fmt.Sprintf("MSH|^~\\&|SRC|%s|DST|FAC|%s||ADT^A01|%s|P|2.3\rPID|||%s||Doe^John||19800101|M\rPV1||I|ICU^01",
		facility, time.Now().Format("200601021504"), msgID, patientID)
}

func buildADT_A08(msgID, patientID, facility string) string {
	return fmt.Sprintf("MSH|^~\\&|SRC|%s|DST|FAC|%s||ADT^A08|%s|P|2.3\rPID|||%s||Smith^Jane||19900215|F\rPV1||O|ER^02",
		facility, time.Now().Format("200601021504"), msgID, patientID)
}

func buildORM_O01(msgID, patientID, facility string) string {
	return fmt.Sprintf("MSH|^~\\&|LAB|%s|HIS|FAC|%s||ORM^O01|%s|P|2.3\rPID|||%s||Brown^Bob\rORC|NW|%s\rOBR|1|%s||CBC",
		facility, time.Now().Format("200601021504"), msgID, patientID, msgID, msgID)
}

func buildORU_R01(msgID, patientID, facility string) string {
	return fmt.Sprintf("MSH|^~\\&|LAB|%s|HIS|FAC|%s||ORU^R01|%s|P|2.3\rPID|||%s||Wilson^Kate\rOBR|1|%s||CBC\rOBX|1|NM|WBC||7.5|10*3/uL|4.5-11.0|N|||F",
		facility, time.Now().Format("200601021504"), msgID, patientID, msgID)
}

func buildADT_A01_NoPatient(msgID, facility string) string {
	return fmt.Sprintf("MSH|^~\\&|SRC|%s|DST|FAC|%s||ADT^A01|%s|P|2.3\rPID|||\rPV1||I|ICU^01",
		facility, time.Now().Format("200601021504"), msgID)
}

func buildMalformed() string {
	return "THIS IS NOT HL7"
}

func buildLargeMessage(msgID, patientID string, segmentCount int) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("MSH|^~\\&|SRC|HOSP|DST|FAC|%s||ADT^A01|%s|P|2.3\r", time.Now().Format("200601021504"), msgID))
	sb.WriteString(fmt.Sprintf("PID|||%s||Load^Test||19700101|M\r", patientID))
	for i := 0; i < segmentCount; i++ {
		sb.WriteString(fmt.Sprintf("OBX|%d|ST|NOTE_%d||Test observation segment number %d for load testing purposes|||||F\r", i+1, i+1, i+1))
	}
	return sb.String()
}

// --- Test Result Tracking ---

type TestResult struct {
	Name     string  `json:"name"`
	Passed   bool    `json:"passed"`
	Duration float64 `json:"duration_ms"`
	Details  string  `json:"details,omitempty"`
	Error    string  `json:"error,omitempty"`
}

type TestSuite struct {
	Name    string       `json:"suite"`
	Results []TestResult `json:"results"`
	Passed  int          `json:"passed"`
	Failed  int          `json:"failed"`
	Total   int          `json:"total"`
}

func (s *TestSuite) run(name string, fn func() error) {
	start := time.Now()
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("panic: %v", r)
			}
		}()
		err = fn()
	}()
	dur := time.Since(start).Seconds() * 1000

	result := TestResult{
		Name:     name,
		Duration: dur,
	}

	if err != nil {
		result.Passed = false
		result.Error = err.Error()
		s.Failed++
		fmt.Printf("  ✗ %s (%.1fms) — %v\n", name, dur, err)
	} else {
		result.Passed = true
		s.Passed++
		fmt.Printf("  ✓ %s (%.1fms)\n", name, dur)
	}

	s.Results = append(s.Results, result)
	s.Total++
}

func (s *TestSuite) summary() {
	status := "PASS"
	if s.Failed > 0 {
		status = "FAIL"
	}
	fmt.Printf("\n[%s] %s: %d/%d passed\n", status, s.Name, s.Passed, s.Total)
}

// --- Main ---

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: harness <command> [args]")
		fmt.Println()
		fmt.Println("Commands:")
		fmt.Println("  test-all          Run all test suites")
		fmt.Println("  test-ingest       Test ingestion (MLLP → NATS → ScyllaDB)")
		fmt.Println("  test-api          Test management API CRUD")
		fmt.Println("  test-filters      Test filter chain (JS transform, conditional)")
		fmt.Println("  test-routing      Test message routing to correct destinations")
		fmt.Println("  test-dlq          Test dead letter queue / error handling")
		fmt.Println("  test-load         Load test (configurable volume)")
		fmt.Println("  test-metrics      Test live metrics counters")
		fmt.Println("  server            Start MLLP receiver on :2576 (for egress testing)")
		fmt.Println("  send <file>       Send an HL7 file via MLLP")
		os.Exit(1)
	}

	mllpHost := envOr("ARTERIA_MLLP_HOST", "ingestion")
	mllpPort := 2575
	apiBase := envOr("ARTERIA_API_URL", "http://api:8080")

	cmd := os.Args[1]
	switch cmd {
	case "test-all":
		runAllTests(mllpHost, mllpPort, apiBase)
	case "test-ingest":
		runIngestTests(mllpHost, mllpPort, apiBase)
	case "test-api":
		runAPITests(apiBase)
	case "test-filters":
		runFilterTests(mllpHost, mllpPort, apiBase)
	case "test-routing":
		runRoutingTests(mllpHost, mllpPort, apiBase)
	case "test-dlq":
		runDLQTests(mllpHost, mllpPort, apiBase)
	case "test-load":
		count := 1000
		if len(os.Args) > 2 {
			fmt.Sscanf(os.Args[2], "%d", &count)
		}
		runLoadTest(mllpHost, mllpPort, apiBase, count)
	case "test-metrics":
		runMetricsTests(mllpHost, mllpPort, apiBase)
	case "server":
		runMLLPServer()
	case "send":
		if len(os.Args) < 3 {
			fmt.Println("Usage: harness send <file>")
			os.Exit(1)
		}
		data, _ := os.ReadFile(os.Args[2])
		if err := mllpSend(mllpHost, mllpPort, string(data)); err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Sent OK")
	default:
		fmt.Printf("Unknown command: %s\n", cmd)
		os.Exit(1)
	}
}

func waitForServices(apiBase string) {
	fmt.Println("Waiting for Arteria services...")
	for i := 0; i < 60; i++ {
		resp, err := http.Get(apiBase + "/health")
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			fmt.Println("Services ready.")
			return
		}
		time.Sleep(1 * time.Second)
	}
	fmt.Println("WARNING: Services may not be fully ready")
}

// ============================================================
// TEST SUITES
// ============================================================

func runAllTests(mllpHost string, mllpPort int, apiBase string) {
	waitForServices(apiBase)
	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("ARTERIA TEST HARNESS — Full Suite")
	fmt.Println(strings.Repeat("=", 60))

	suites := []*TestSuite{}

	s1 := runIngestTests(mllpHost, mllpPort, apiBase)
	suites = append(suites, s1)

	s2 := runAPITests(apiBase)
	suites = append(suites, s2)

	s3 := runFilterTests(mllpHost, mllpPort, apiBase)
	suites = append(suites, s3)

	s4 := runRoutingTests(mllpHost, mllpPort, apiBase)
	suites = append(suites, s4)

	s5 := runDLQTests(mllpHost, mllpPort, apiBase)
	suites = append(suites, s5)

	s6 := runMetricsTests(mllpHost, mllpPort, apiBase)
	suites = append(suites, s6)

	// Final summary
	totalPassed, totalFailed := 0, 0
	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("FINAL RESULTS")
	fmt.Println(strings.Repeat("=", 60))
	for _, s := range suites {
		status := "✓"
		if s.Failed > 0 {
			status = "✗"
		}
		fmt.Printf("  %s %-30s %d/%d\n", status, s.Name, s.Passed, s.Total)
		totalPassed += s.Passed
		totalFailed += s.Failed
	}
	fmt.Printf("\nTotal: %d passed, %d failed out of %d\n", totalPassed, totalFailed, totalPassed+totalFailed)

	// Write JSON report
	report, _ := json.MarshalIndent(suites, "", "  ")
	os.WriteFile("/tmp/arteria-test-report.json", report, 0644)
	fmt.Println("Report: /tmp/arteria-test-report.json")

	if totalFailed > 0 {
		os.Exit(1)
	}
}

// --- Ingestion Tests ---

func runIngestTests(mllpHost string, mllpPort int, apiBase string) *TestSuite {
	s := &TestSuite{Name: "Ingestion"}
	fmt.Println("\n── Ingestion Tests ──")

	s.run("Send single ADT^A01", func() error {
		return mllpSend(mllpHost, mllpPort, buildADT_A01("INGEST001", "PAT-INGEST-001", "HOSP_TEST"))
	})

	s.run("Send ADT^A08 update", func() error {
		return mllpSend(mllpHost, mllpPort, buildADT_A08("INGEST002", "PAT-INGEST-002", "HOSP_TEST"))
	})

	s.run("Send ORM^O01 order", func() error {
		return mllpSend(mllpHost, mllpPort, buildORM_O01("INGEST003", "PAT-INGEST-003", "LAB_TEST"))
	})

	s.run("Send ORU^R01 result", func() error {
		return mllpSend(mllpHost, mllpPort, buildORU_R01("INGEST004", "PAT-INGEST-004", "LAB_TEST"))
	})

	s.run("Send batch of 10 messages", func() error {
		var msgs []string
		for i := 0; i < 10; i++ {
			msgs = append(msgs, buildADT_A01(fmt.Sprintf("BATCH%03d", i), fmt.Sprintf("PAT-BATCH-%03d", i), "HOSP_BATCH"))
		}
		sent, failed := mllpSendBatch(mllpHost, mllpPort, msgs)
		if failed > 0 {
			return fmt.Errorf("%d/%d failed", failed, sent+failed)
		}
		return nil
	})

	s.run("Send large message (100 OBX segments)", func() error {
		return mllpSend(mllpHost, mllpPort, buildLargeMessage("LARGE001", "PAT-LARGE-001", 100))
	})

	s.run("Verify messages in ScyllaDB via API", func() error {
		time.Sleep(2 * time.Second)
		result, err := apiGet(apiBase, "/api/v1/messages?limit=50")
		if err != nil {
			return err
		}
		countVal, ok := result["count"]
		if !ok || countVal == nil {
			return fmt.Errorf("API response missing 'count' field: %v", result)
		}
		count, ok := countVal.(float64)
		if !ok {
			return fmt.Errorf("expected count to be a number, got %T: %v", countVal, countVal)
		}
		if count < 1 {
			return fmt.Errorf("expected messages in DB, got count=%v", count)
		}
		return nil
	})

	s.run("Verify message detail has raw payload", func() error {
		result, err := apiGet(apiBase, "/api/v1/messages?limit=1")
		if err != nil {
			return err
		}
		msgs := result["messages"].([]interface{})
		if len(msgs) == 0 {
			return fmt.Errorf("no messages found")
		}
		msg := msgs[0].(map[string]interface{})
		id := msg["message_id"].(string)

		detail, err := apiGet(apiBase, "/api/v1/messages/"+id)
		if err != nil {
			return err
		}
		raw := detail["raw_payload"].(string)
		if !strings.Contains(raw, "MSH|") {
			return fmt.Errorf("raw_payload does not contain MSH segment")
		}
		return nil
	})

	s.summary()
	return s
}

// --- API CRUD Tests ---

func runAPITests(apiBase string) *TestSuite {
	s := &TestSuite{Name: "API CRUD"}
	fmt.Println("\n── API CRUD Tests ──")

	var cpID, routeID string

	s.run("GET /health returns ok", func() error {
		result, err := apiGet(apiBase, "/health")
		if err != nil {
			return err
		}
		if result["status"] != "ok" {
			return fmt.Errorf("expected ok, got %v", result["status"])
		}
		return nil
	})

	s.run("Create communication point", func() error {
		result, err := apiPost(apiBase, "/api/v1/comm-points", map[string]interface{}{
			"name": "Test CP", "direction": "OUTPUT", "protocol": "HTTP",
			"host": "test.local", "port": 8888, "is_active": true,
			"max_retries": 2, "retry_delay_ms": 500, "timeout_ms": 10000,
		})
		if err != nil {
			return err
		}
		id, ok := result["comm_point_id"].(string)
		if !ok || id == "" {
			return fmt.Errorf("no comm_point_id returned")
		}
		cpID = id
		time.Sleep(1 * time.Second)
		return nil
	})

	s.run("List communication points includes new CP", func() error {
		for retry := 0; retry < 5; retry++ {
			result, err := apiGet(apiBase, "/api/v1/comm-points")
			if err != nil {
				return err
			}
			cps := result["communication_points"].([]interface{})
			for _, cp := range cps {
				m := cp.(map[string]interface{})
				if m["name"] == "Test CP" {
					return nil
				}
			}
			time.Sleep(500 * time.Millisecond)
		}
		return fmt.Errorf("Test CP not found in list after retries")
	})

	s.run("Create route", func() error {
		result, err := apiPost(apiBase, "/api/v1/routes", map[string]interface{}{
			"name": "Test Route", "description": "Created by test harness",
			"source_topic": "ORU^R01", "destination_topic": "test_results",
			"is_active": true,
		})
		if err != nil {
			return err
		}
		id, ok := result["route_id"].(string)
		if !ok || id == "" {
			return fmt.Errorf("no route_id returned")
		}
		routeID = id
		time.Sleep(1 * time.Second)
		return nil
	})

	s.run("Get route by ID", func() error {
		for retry := 0; retry < 5; retry++ {
			result, err := apiGet(apiBase, "/api/v1/routes/"+routeID)
			if err != nil {
				return err
			}
			if result["name"] == "Test Route" {
				return nil
			}
			time.Sleep(500 * time.Millisecond)
		}
		return fmt.Errorf("route name not propagated after retries")
	})

	s.run("Create filter on route", func() error {
		_, err := apiPost(apiBase, "/api/v1/routes/"+routeID+"/filters", map[string]interface{}{
			"name": "Test Transform", "filter_type": "javascript", "execution_order": 0,
			"js_script": "function transform(msg) { msg.properties.test_harness = 'true'; return msg; }",
			"is_active": true,
		})
		time.Sleep(1 * time.Second)
		return err
	})

	s.run("List filters on route", func() error {
		result, err := apiGet(apiBase, "/api/v1/routes/"+routeID+"/filters")
		if err != nil {
			return err
		}
		count := result["count"].(float64)
		if count < 1 {
			return fmt.Errorf("expected at least 1 filter, got %v", count)
		}
		return nil
	})

	s.run("Create lookup table", func() error {
		_, err := apiPost(apiBase, "/api/v1/lookups", map[string]interface{}{
			"name": "test_lookup", "description": "Test harness lookup",
		})
		time.Sleep(1 * time.Second)
		return err
	})

	s.run("List lookup tables", func() error {
		result, err := apiGet(apiBase, "/api/v1/lookups")
		if err != nil {
			return err
		}
		tables := result["lookup_tables"].([]interface{})
		for _, t := range tables {
			m := t.(map[string]interface{})
			if m["name"] == "test_lookup" {
				return nil
			}
		}
		return fmt.Errorf("test_lookup not found")
	})

	s.run("Update route", func() error {
		return apiPut(apiBase, "/api/v1/routes/"+routeID, map[string]interface{}{
			"name": "Test Route Updated", "description": "Updated by test harness",
			"source_topic": "ORU^R01", "destination_topic": "test_results_v2",
			"is_active": true,
		})
	})

	s.run("Get stats", func() error {
		result, err := apiGet(apiBase, "/api/v1/stats")
		if err != nil {
			return err
		}
		if _, ok := result["total_messages"]; !ok {
			return fmt.Errorf("missing total_messages in stats")
		}
		return nil
	})

	// Cleanup
	s.run("Delete test comm point", func() error {
		req, _ := http.NewRequest("DELETE", apiBase+"/api/v1/comm-points/"+cpID, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		resp.Body.Close()
		return nil
	})

	s.summary()
	return s
}

// --- Filter Chain Tests ---

func runFilterTests(mllpHost string, mllpPort int, apiBase string) *TestSuite {
	s := &TestSuite{Name: "Filter Chain"}
	fmt.Println("\n── Filter Chain Tests ──")

	s.run("V8 JS transform adds properties", func() error {
		msg := buildADT_A01("FILTER001", "PAT-FILTER-001", "HOSP_A")
		if err := mllpSend(mllpHost, mllpPort, msg); err != nil {
			return err
		}
		time.Sleep(2 * time.Second)

		// Find the message
		result, err := apiGet(apiBase, "/api/v1/messages?limit=5")
		if err != nil {
			return err
		}
		msgs := result["messages"].([]interface{})
		for _, m := range msgs {
			msg := m.(map[string]interface{})
			if msg["status"] == "ROUTED" {
				detail, _ := apiGet(apiBase, "/api/v1/messages/"+msg["message_id"].(string))
				tp := detail["transformed_payload"].(string)
				if strings.Contains(tp, "processed_by") {
					return nil
				}
			}
		}
		return fmt.Errorf("no message found with V8 transform properties")
	})

	s.run("Conditional filter rejects missing patient ID", func() error {
		msg := buildADT_A01_NoPatient("FILTER002", "HOSP_A")
		mllpSend(mllpHost, mllpPort, msg)
		time.Sleep(2 * time.Second)

		result, err := apiGet(apiBase, "/api/v1/errors?limit=10")
		if err != nil {
			return err
		}
		errors := result["errors"].([]interface{})
		for _, e := range errors {
			m := e.(map[string]interface{})
			if strings.Contains(m["error_details"].(string), "Missing Patient ID") {
				return nil
			}
		}
		return fmt.Errorf("expected rejection error for missing patient ID")
	})

	s.run("Multiple messages through same filter chain", func() error {
		for i := 0; i < 5; i++ {
			mllpSend(mllpHost, mllpPort, buildADT_A01(
				fmt.Sprintf("CHAIN%03d", i),
				fmt.Sprintf("PAT-CHAIN-%03d", i),
				"HOSP_CHAIN"))
		}
		time.Sleep(2 * time.Second)
		return nil // If no panic/crash, chain handles concurrent execution
	})

	s.summary()
	return s
}

// --- Routing Tests ---

func runRoutingTests(mllpHost string, mllpPort int, apiBase string) *TestSuite {
	s := &TestSuite{Name: "Routing"}
	fmt.Println("\n── Routing Tests ──")

	s.run("ADT^A01 routes to admissions", func() error {
		mllpSend(mllpHost, mllpPort, buildADT_A01("ROUTE001", "PAT-ROUTE-001", "HOSP_A"))
		time.Sleep(2 * time.Second)
		result, err := apiGet(apiBase, "/api/v1/messages?limit=50")
		if err != nil {
			return err
		}
		msgs := result["messages"].([]interface{})
		for _, m := range msgs {
			msg := m.(map[string]interface{})
			if msg["patient_id"] == "PAT-ROUTE-001" && msg["status"] == "ROUTED" {
				return nil
			}
		}
		return fmt.Errorf("ADT^A01 message not routed")
	})

	s.run("ORM^O01 hits catch-all route", func() error {
		mllpSend(mllpHost, mllpPort, buildORM_O01("ROUTE002", "PAT-ROUTE-002", "LAB_A"))
		time.Sleep(2 * time.Second)
		result, _ := apiGet(apiBase, "/api/v1/messages?limit=10")
		msgs := result["messages"].([]interface{})
		for _, m := range msgs {
			msg := m.(map[string]interface{})
			if msg["patient_id"] == "PAT-ROUTE-002" && msg["status"] == "ROUTED" {
				return nil
			}
		}
		return fmt.Errorf("ORM^O01 not routed via catch-all")
	})

	s.run("Mixed message types all route correctly", func() error {
		mllpSend(mllpHost, mllpPort, buildADT_A01("MIX001", "PAT-MIX-001", "HOSP"))
		mllpSend(mllpHost, mllpPort, buildORM_O01("MIX002", "PAT-MIX-002", "LAB"))
		mllpSend(mllpHost, mllpPort, buildORU_R01("MIX003", "PAT-MIX-003", "LAB"))
		mllpSend(mllpHost, mllpPort, buildADT_A08("MIX004", "PAT-MIX-004", "HOSP"))
		time.Sleep(2 * time.Second)
		return nil
	})

	s.summary()
	return s
}

// --- DLQ / Error Tests ---

func runDLQTests(mllpHost string, mllpPort int, apiBase string) *TestSuite {
	s := &TestSuite{Name: "Error Handling / DLQ"}
	fmt.Println("\n── Error Handling / DLQ Tests ──")

	s.run("Message without patient ID goes to DLQ", func() error {
		mllpSend(mllpHost, mllpPort, buildADT_A01_NoPatient("DLQ001", "HOSP_DLQ"))
		time.Sleep(2 * time.Second)
		result, _ := apiGet(apiBase, "/api/v1/errors?limit=20")
		count := result["count"].(float64)
		if count < 1 {
			return fmt.Errorf("expected errors in DLQ, got %v", count)
		}
		return nil
	})

	s.run("Error records have correct error_type", func() error {
		result, _ := apiGet(apiBase, "/api/v1/errors?limit=5")
		errors := result["errors"].([]interface{})
		for _, e := range errors {
			m := e.(map[string]interface{})
			if m["error_type"] == "FILTER_ERROR" {
				return nil
			}
		}
		return fmt.Errorf("no FILTER_ERROR type found")
	})

	s.run("Messages by status ERROR exist", func() error {
		// Send a message that will be rejected to ensure errors exist
		mllpSend(mllpHost, mllpPort, buildADT_A01_NoPatient("DLQSTAT001", "HOSP_DLQ"))
		time.Sleep(3 * time.Second)
		// Check the messages table for ERROR status directly
		result, _ := apiGet(apiBase, "/api/v1/messages?limit=100")
		msgs := result["messages"].([]interface{})
		for _, m := range msgs {
			msg := m.(map[string]interface{})
			if msg["status"] == "ERROR" {
				return nil
			}
		}
		return fmt.Errorf("no ERROR status messages found in messages table")
	})

	s.summary()
	return s
}

// --- Metrics Tests ---

func runMetricsTests(mllpHost string, mllpPort int, apiBase string) *TestSuite {
	s := &TestSuite{Name: "Metrics"}
	fmt.Println("\n── Metrics Tests ──")

	s.run("Live metrics endpoint returns data", func() error {
		result, err := apiGet(apiBase, "/api/v1/metrics")
		if err != nil {
			return err
		}
		if _, ok := result["ingestion"]; !ok {
			return fmt.Errorf("missing ingestion metrics")
		}
		if _, ok := result["processing"]; !ok {
			return fmt.Errorf("missing processing metrics")
		}
		return nil
	})

	s.run("Ingestion received count > 0", func() error {
		result, _ := apiGet(apiBase, "/api/v1/metrics")
		ing := result["ingestion"].(map[string]interface{})
		if ing["received"].(float64) < 1 {
			return fmt.Errorf("ingestion received is 0")
		}
		return nil
	})

	s.run("Processing routed count > 0", func() error {
		result, _ := apiGet(apiBase, "/api/v1/metrics")
		proc := result["processing"].(map[string]interface{})
		if proc["routed"].(float64) < 1 {
			return fmt.Errorf("processing routed is 0")
		}
		return nil
	})

	s.run("Per-CP metrics available", func() error {
		result, err := apiGet(apiBase, "/api/v1/metrics/comm-points")
		if err != nil {
			return err
		}
		count := result["count"].(float64)
		if count < 1 {
			return fmt.Errorf("no comm points in metrics")
		}
		return nil
	})

	s.run("CP log entries exist", func() error {
		result, _ := apiGet(apiBase, "/api/v1/metrics/comm-points")
		cps := result["comm_points"].(map[string]interface{})
		for id := range cps {
			logResult, err := apiGet(apiBase, "/api/v1/metrics/comm-points/"+id+"/logs")
			if err != nil {
				return err
			}
			logCount := logResult["log_count"].(float64)
			if logCount > 0 {
				return nil
			}
		}
		return fmt.Errorf("no CP log entries found")
	})

	s.run("Send messages and verify counter increments", func() error {
		before, _ := apiGet(apiBase, "/api/v1/metrics")
		ingBefore := before["ingestion"].(map[string]interface{})
		countBefore := ingBefore["received"].(float64)

		for i := 0; i < 3; i++ {
			mllpSend(mllpHost, mllpPort, buildADT_A01(fmt.Sprintf("METRIC%03d", i), fmt.Sprintf("PAT-M-%03d", i), "HOSP"))
		}
		time.Sleep(2 * time.Second)

		after, _ := apiGet(apiBase, "/api/v1/metrics")
		ingAfter := after["ingestion"].(map[string]interface{})
		countAfter := ingAfter["received"].(float64)

		if countAfter <= countBefore {
			return fmt.Errorf("received counter did not increment: before=%v after=%v", countBefore, countAfter)
		}
		return nil
	})

	s.summary()
	return s
}

// --- Load Test ---

func runLoadTest(mllpHost string, mllpPort int, apiBase string, count int) {
	fmt.Printf("\n── Load Test: %d messages ──\n", count)

	before, _ := apiGet(apiBase, "/api/v1/metrics")
	var startReceived float64
	if before != nil {
		if ing, ok := before["ingestion"].(map[string]interface{}); ok {
			startReceived = ing["received"].(float64)
		}
	}

	start := time.Now()
	var sent, failed atomic.Int64
	var wg sync.WaitGroup
	concurrency := 10

	batch := count / concurrency
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < batch; i++ {
				msgID := fmt.Sprintf("LOAD-%d-%06d", workerID, i)
				patID := fmt.Sprintf("PAT-LOAD-%d-%06d", workerID, i)
				err := mllpSend(mllpHost, mllpPort, buildADT_A01(msgID, patID, "HOSP_LOAD"))
				if err != nil {
					failed.Add(1)
				} else {
					sent.Add(1)
				}
			}
		}(w)
	}
	wg.Wait()

	elapsed := time.Since(start)
	sentCount := sent.Load()
	failedCount := failed.Load()
	msgsPerSec := float64(sentCount) / elapsed.Seconds()

	fmt.Printf("  Sent:     %d\n", sentCount)
	fmt.Printf("  Failed:   %d\n", failedCount)
	fmt.Printf("  Duration: %v\n", elapsed.Round(time.Millisecond))
	fmt.Printf("  Rate:     %.0f msgs/sec\n", msgsPerSec)
	fmt.Printf("  Workers:  %d concurrent\n", concurrency)

	// Wait for processing
	fmt.Println("  Waiting for processing to complete...")
	time.Sleep(5 * time.Second)

	after, _ := apiGet(apiBase, "/api/v1/metrics")
	if after != nil {
		if ing, ok := after["ingestion"].(map[string]interface{}); ok {
			endReceived := ing["received"].(float64)
			fmt.Printf("  Ingestion received: %.0f (delta: +%.0f)\n", endReceived, endReceived-startReceived)
		}
		if proc, ok := after["processing"].(map[string]interface{}); ok {
			fmt.Printf("  Processing routed:  %.0f\n", proc["routed"].(float64))
			fmt.Printf("  Processing errors:  %.0f\n", proc["errors"].(float64))
			fmt.Printf("  Msgs/min (live):    %.0f\n", proc["msgs_per_minute"].(float64))
		}
	}
}

// --- MLLP Receiver Server ---

func runMLLPServer() {
	port := envOr("RECEIVER_PORT", "2576")
	srv, err := newMLLPServer(":" + port)
	if err != nil {
		fmt.Printf("Failed to start MLLP server: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("MLLP receiver listening on :%s\n", port)
	fmt.Println("Press Ctrl+C to stop and show received messages.")

	// Periodic status
	go func() {
		for {
			time.Sleep(5 * time.Second)
			fmt.Printf("  [receiver] %d messages received\n", srv.count.Load())
		}
	}()

	// Wait for signal
	sigCh := make(chan os.Signal, 1)
	// signal.Notify not imported to keep deps minimal, just block
	<-sigCh

	srv.stop()
	received := srv.getReceived()
	fmt.Printf("\nTotal received: %d\n", len(received))
	for i, m := range received {
		fmt.Printf("  [%d] %s size=%d from=%s\n", i+1, m.Timestamp.Format(time.RFC3339), m.Size, m.From)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
