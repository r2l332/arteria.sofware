package main

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	mrand "math/rand"
	"net"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// Production Performance Test
// Simulates realistic hospital traffic patterns across multiple CPs

const (
	startBlock = 0x0B
	endBlock   = 0x1C
	cr         = 0x0D
)

// --- Configuration ---

type PerfConfig struct {
	TargetHost     string
	Ports          []int    // Multiple CPs (simulates different departments)
	Duration       time.Duration
	Concurrency    int
	RampUpSeconds  int
	MessageMix     []MessageProfile
	BurstEnabled   bool
	BurstInterval  time.Duration
	BurstSize      int
	ReportInterval time.Duration
}

type MessageProfile struct {
	Type        string  // ADT^A01, ORM^O01, ORU^R01, etc.
	Weight      float64 // Proportion of traffic (0.0-1.0)
	MinSegments int     // Min OBX/OBR segments
	MaxSegments int     // Max OBX/OBR segments
}

// --- Metrics ---

type PerfMetrics struct {
	Sent        atomic.Int64
	Failed      atomic.Int64
	BytesSent   atomic.Int64
	Latencies   sync.Map // stores []time.Duration per port
	StartTime   time.Time
	PerPort     [8]PortMetrics
}

type PortMetrics struct {
	Sent   atomic.Int64
	Failed atomic.Int64
	Bytes  atomic.Int64
}

// --- Results ---

type PerfReport struct {
	Duration       time.Duration
	TotalSent      int64
	TotalFailed    int64
	TotalBytes     int64
	MsgsPerSecond  float64
	MBPerSecond    float64
	P50Latency     time.Duration
	P95Latency     time.Duration
	P99Latency     time.Duration
	PerPort        []PortReport
	MessageSizes   SizeStats
}

type PortReport struct {
	Port      int
	Sent      int64
	Failed    int64
	BytesSent int64
	Rate      float64
}

type SizeStats struct {
	Min int64
	Max int64
	Avg int64
}

var authToken string

func main() {
	config := parseConfig()

	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║     ARTERIA PRODUCTION PERFORMANCE TEST                     ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════╣")
	fmt.Printf("║  Target:      %s\n", config.TargetHost)
	fmt.Printf("║  Ports:       %v\n", config.Ports)
	fmt.Printf("║  Duration:    %v\n", config.Duration)
	fmt.Printf("║  Concurrency: %d workers per port\n", config.Concurrency)
	fmt.Printf("║  Ramp-up:     %ds\n", config.RampUpSeconds)
	fmt.Printf("║  Burst:       %v (every %v, %d msgs)\n", config.BurstEnabled, config.BurstInterval, config.BurstSize)
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Println()

	// Authenticate
	apiBase := envOr("API_URL", "http://"+config.TargetHost+":8080")
	if err := login(apiBase); err != nil {
		fmt.Printf("WARNING: Could not authenticate: %v\n", err)
	}

	// Get baseline metrics
	baseline := getMetrics(apiBase)

	// Run the test
	metrics := runPerfTest(config)

	// Get final metrics
	fmt.Println("\nWaiting for processing pipeline to drain...")
	time.Sleep(10 * time.Second)
	final := getMetrics(apiBase)

	// Generate report
	report := generateReport(config, metrics, baseline, final)
	printReport(report)
}

func parseConfig() PerfConfig {
	host := envOr("TARGET_HOST", "localhost")
	duration := parseDuration(envOr("DURATION", "60s"))
	concurrency := parseInt(envOr("CONCURRENCY", "20"))
	rampUp := parseInt(envOr("RAMP_UP", "5"))

	ports := []int{2575, 2576, 2577, 2578}
	if p := os.Getenv("PORTS"); p != "" {
		ports = parsePorts(p)
	}

	return PerfConfig{
		TargetHost:    host,
		Ports:         ports,
		Duration:      duration,
		Concurrency:   concurrency,
		RampUpSeconds: rampUp,
		MessageMix: []MessageProfile{
			{Type: "ADT^A01", Weight: 0.25, MinSegments: 2, MaxSegments: 5},   // Admissions
			{Type: "ADT^A08", Weight: 0.15, MinSegments: 2, MaxSegments: 4},   // Updates
			{Type: "ADT^A03", Weight: 0.10, MinSegments: 1, MaxSegments: 3},   // Discharges
			{Type: "ORM^O01", Weight: 0.20, MinSegments: 3, MaxSegments: 15},  // Lab Orders
			{Type: "ORU^R01", Weight: 0.20, MinSegments: 5, MaxSegments: 100}, // Results (large)
			{Type: "SIU^S12", Weight: 0.05, MinSegments: 2, MaxSegments: 6},   // Scheduling
			{Type: "MDM^T02", Weight: 0.05, MinSegments: 1, MaxSegments: 50},  // Documents
		},
		BurstEnabled:   true,
		BurstInterval:  15 * time.Second,
		BurstSize:      200,
		ReportInterval: 5 * time.Second,
	}
}

func runPerfTest(config PerfConfig) *PerfMetrics {
	metrics := &PerfMetrics{StartTime: time.Now()}
	deadline := time.Now().Add(config.Duration)
	var wg sync.WaitGroup

	// Progress reporter
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(config.ReportInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				elapsed := time.Since(metrics.StartTime)
				sent := metrics.Sent.Load()
				failed := metrics.Failed.Load()
				rate := float64(sent) / elapsed.Seconds()
				mb := float64(metrics.BytesSent.Load()) / (1024 * 1024)
				fmt.Printf("  [%v] sent=%d failed=%d rate=%.0f msgs/s throughput=%.1f MB/s\n",
					elapsed.Round(time.Second), sent, failed, rate, mb/elapsed.Seconds())
			}
		}
	}()

	// Launch workers per port with ramp-up
	for portIdx, port := range config.Ports {
		for w := 0; w < config.Concurrency; w++ {
			wg.Add(1)
			workerDelay := time.Duration(float64(config.RampUpSeconds) * float64(w) / float64(config.Concurrency) * float64(time.Second))
			go func(pIdx, p, workerID int, delay time.Duration) {
				defer wg.Done()
				time.Sleep(delay) // Ramp-up

				for time.Now().Before(deadline) {
					msg := generateMessage(config.MessageMix, workerID, p)
					msgBytes := len(msg)

					start := time.Now()
					err := mllpSend(config.TargetHost, p, msg)
					latency := time.Since(start)

					if err != nil {
						metrics.Failed.Add(1)
						metrics.PerPort[pIdx].Failed.Add(1)
					} else {
						metrics.Sent.Add(1)
						metrics.BytesSent.Add(int64(msgBytes))
						metrics.PerPort[pIdx].Sent.Add(1)
						metrics.PerPort[pIdx].Bytes.Add(int64(msgBytes))
						_ = latency // Could store for percentile calc
					}

					// Realistic inter-message delay (1-50ms, simulating real hospital systems)
					jitter := time.Duration(randInt(1, 50)) * time.Millisecond
					time.Sleep(jitter)
				}
			}(portIdx, port, w, workerDelay)
		}
	}

	// Burst generator (simulates shift change, batch uploads, etc.)
	if config.BurstEnabled {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ticker := time.NewTicker(config.BurstInterval)
			defer ticker.Stop()
			for {
				select {
				case <-time.After(time.Until(deadline)):
					return
				case <-ticker.C:
					if time.Now().After(deadline) {
						return
					}
					fmt.Printf("  [BURST] Sending %d messages across all ports\n", config.BurstSize)
					for i := 0; i < config.BurstSize; i++ {
						port := config.Ports[i%len(config.Ports)]
						pIdx := i % len(config.Ports)
						msg := generateMessage(config.MessageMix, 999, port)
						if err := mllpSend(config.TargetHost, port, msg); err != nil {
							metrics.Failed.Add(1)
							metrics.PerPort[pIdx].Failed.Add(1)
						} else {
							metrics.Sent.Add(1)
							metrics.BytesSent.Add(int64(len(msg)))
							metrics.PerPort[pIdx].Sent.Add(1)
							metrics.PerPort[pIdx].Bytes.Add(int64(len(msg)))
						}
					}
				}
			}
		}()
	}

	wg.Wait()
	close(done)
	return metrics
}

func generateMessage(mix []MessageProfile, workerID, port int) string {
	// Weighted random selection
	r := mrand.Float64()
	var cumulative float64
	var profile MessageProfile
	for _, p := range mix {
		cumulative += p.Weight
		if r <= cumulative {
			profile = p
			break
		}
	}
	if profile.Type == "" {
		profile = mix[0]
	}

	msgID := fmt.Sprintf("PERF-%d-%d-%d", port, workerID, time.Now().UnixNano()%1000000)
	patID := fmt.Sprintf("MRN%08d", randInt(10000000, 99999999))
	facility := randomFacility()
	numSegments := randInt(profile.MinSegments, profile.MaxSegments)

	return buildHL7Message(profile.Type, msgID, patID, facility, numSegments)
}

func buildHL7Message(msgType, msgID, patID, facility string, segments int) string {
	parts := splitMsgType(msgType)
	now := time.Now().Format("20060102150405")

	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("MSH|^~\\&|SRC|%s|DST|FAC|%s||%s|%s|P|2.3\r", facility, now, msgType, msgID))
	buf.WriteString(fmt.Sprintf("PID|||%s||%s^%s||%s|%s\r", patID, randomLastName(), randomFirstName(), randomDOB(), randomGender()))

	switch parts[0] {
	case "ADT":
		buf.WriteString(fmt.Sprintf("PV1||%s|%s^%s\r", randomPatClass(), randomWard(), randomBed()))
		for i := 0; i < segments-2; i++ {
			buf.WriteString(fmt.Sprintf("NK1|%d|%s^%s|%s\r", i+1, randomLastName(), randomFirstName(), randomRelation()))
		}
	case "ORM":
		buf.WriteString(fmt.Sprintf("ORC|NW|%s\r", msgID))
		for i := 0; i < segments-2; i++ {
			buf.WriteString(fmt.Sprintf("OBR|%d|%s||%s|||%s\r", i+1, msgID, randomLabTest(), now))
		}
	case "ORU":
		buf.WriteString(fmt.Sprintf("OBR|1|%s||%s|||%s\r", msgID, randomLabTest(), now))
		for i := 0; i < segments-1; i++ {
			buf.WriteString(fmt.Sprintf("OBX|%d|NM|%s||%s|%s|%s|%s|||F\r",
				i+1, randomLabCode(), randomNumericResult(), randomUnit(), randomRange(), randomFlag()))
		}
	case "SIU":
		buf.WriteString(fmt.Sprintf("SCH|%s||||||%s|%s\r", msgID, now, randomDuration()))
		buf.WriteString(fmt.Sprintf("RGS|1\r"))
		buf.WriteString(fmt.Sprintf("AIS|1||%s\r", randomService()))
	case "MDM":
		buf.WriteString(fmt.Sprintf("TXA|1|%s||%s|||||||||||%s\r", randomDocType(), now, msgID))
		for i := 0; i < segments; i++ {
			buf.WriteString(fmt.Sprintf("OBX|%d|TX|||%s\r", i+1, randomText(randInt(50, 500))))
		}
	}

	return buf.String()
}

// --- Randomization Helpers ---

var (
	lastNames   = []string{"Smith", "Johnson", "Williams", "Brown", "Jones", "Garcia", "Miller", "Davis", "Rodriguez", "Martinez", "Hernandez", "Lopez", "Gonzalez", "Wilson", "Anderson", "Thomas", "Taylor", "Moore", "Jackson", "Martin"}
	firstNames  = []string{"James", "Mary", "Robert", "Patricia", "John", "Jennifer", "Michael", "Linda", "David", "Elizabeth", "William", "Barbara", "Richard", "Susan", "Joseph", "Jessica", "Thomas", "Sarah", "Charles", "Karen"}
	facilities  = []string{"HOSP_A", "HOSP_B", "CLINIC_NORTH", "CLINIC_SOUTH", "LAB_CENTRAL", "LAB_WEST", "ER_MAIN", "ICU_1", "RADIOLOGY", "CARDIOLOGY", "ONCOLOGY", "ORTHO", "NEURO", "PEDS"}
	wards       = []string{"ICU", "ER", "MED", "SURG", "PEDS", "OB", "PSYCH", "REHAB", "CARD", "ONCO"}
	labTests    = []string{"CBC", "BMP", "CMP", "TSH", "HBA1C", "LIPID", "PT_INR", "UA", "BNP", "TROP", "DIMER", "CRP", "ESR", "FERRITIN", "B12"}
	labCodes    = []string{"WBC", "RBC", "HGB", "HCT", "PLT", "Na", "K", "Cl", "CO2", "BUN", "Cr", "Glu", "Ca", "Mg", "Phos"}
	units       = []string{"10*3/uL", "10*6/uL", "g/dL", "%", "10*3/uL", "mmol/L", "mmol/L", "mmol/L", "mmol/L", "mg/dL", "mg/dL", "mg/dL", "mg/dL", "mg/dL", "mg/dL"}
	docTypes    = []string{"DS", "HP", "OP", "CN", "PR", "RA"}
	services    = []string{"SURGERY", "CONSULT", "FOLLOWUP", "IMAGING", "LAB", "THERAPY"}
	relations   = []string{"SPO", "PAR", "SIB", "CHD", "OTH"}
)

func randomLastName() string  { return lastNames[mrand.Intn(len(lastNames))] }
func randomFirstName() string { return firstNames[mrand.Intn(len(firstNames))] }
func randomFacility() string  { return facilities[mrand.Intn(len(facilities))] }
func randomWard() string      { return wards[mrand.Intn(len(wards))] }
func randomLabTest() string   { return labTests[mrand.Intn(len(labTests))] }
func randomLabCode() string   { return labCodes[mrand.Intn(len(labCodes))] }
func randomUnit() string      { return units[mrand.Intn(len(units))] }
func randomDocType() string   { return docTypes[mrand.Intn(len(docTypes))] }
func randomService() string   { return services[mrand.Intn(len(services))] }
func randomRelation() string  { return relations[mrand.Intn(len(relations))] }

func randomBed() string    { return fmt.Sprintf("%02d", mrand.Intn(30)+1) }
func randomGender() string { return []string{"M", "F"}[mrand.Intn(2)] }
func randomPatClass() string { return []string{"I", "O", "E", "P"}[mrand.Intn(4)] }
func randomDOB() string {
	year := 1940 + mrand.Intn(70)
	month := mrand.Intn(12) + 1
	day := mrand.Intn(28) + 1
	return fmt.Sprintf("%04d%02d%02d", year, month, day)
}
func randomDuration() string { return fmt.Sprintf("%d", 15+mrand.Intn(120)) }
func randomNumericResult() string { return fmt.Sprintf("%.1f", mrand.Float64()*100) }
func randomRange() string { return fmt.Sprintf("%.1f-%.1f", mrand.Float64()*5, 5+mrand.Float64()*10) }
func randomFlag() string { return []string{"N", "N", "N", "N", "H", "L", "HH", "LL"}[mrand.Intn(8)] }
func randomText(length int) string {
	const chars = "abcdefghijklmnopqrstuvwxyz ABCDEFGHIJKLMNOPQRSTUVWXYZ 0123456789 "
	b := make([]byte, length)
	for i := range b {
		b[i] = chars[mrand.Intn(len(chars))]
	}
	return string(b)
}

func splitMsgType(mt string) []string {
	parts := [2]string{"ADT", "A01"}
	if idx := len(mt); idx > 0 {
		for i, c := range mt {
			if c == '^' {
				parts[0] = mt[:i]
				parts[1] = mt[i+1:]
				break
			}
		}
	}
	return parts[:]
}

func randInt(min, max int) int {
	if min >= max {
		return min
	}
	n, _ := rand.Int(rand.Reader, big.NewInt(int64(max-min+1)))
	return min + int(n.Int64())
}

// --- Reporting ---

func generateReport(config PerfConfig, metrics *PerfMetrics, baseline, final map[string]interface{}) PerfReport {
	elapsed := time.Since(metrics.StartTime)
	sent := metrics.Sent.Load()
	bytesTotal := metrics.BytesSent.Load()

	report := PerfReport{
		Duration:      elapsed,
		TotalSent:     sent,
		TotalFailed:   metrics.Failed.Load(),
		TotalBytes:    bytesTotal,
		MsgsPerSecond: float64(sent) / elapsed.Seconds(),
		MBPerSecond:   float64(bytesTotal) / (1024 * 1024) / elapsed.Seconds(),
	}

	for i, port := range config.Ports {
		if i >= 8 {
			break
		}
		ps := metrics.PerPort[i].Sent.Load()
		report.PerPort = append(report.PerPort, PortReport{
			Port:      port,
			Sent:      ps,
			Failed:    metrics.PerPort[i].Failed.Load(),
			BytesSent: metrics.PerPort[i].Bytes.Load(),
			Rate:      float64(ps) / elapsed.Seconds(),
		})
	}

	if sent > 0 {
		report.MessageSizes.Avg = bytesTotal / sent
	}

	return report
}

func printReport(r PerfReport) {
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║              PERFORMANCE TEST RESULTS                        ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════╣")
	fmt.Printf("║  Duration:        %v\n", r.Duration.Round(time.Second))
	fmt.Printf("║  Total Sent:      %d messages\n", r.TotalSent)
	fmt.Printf("║  Total Failed:    %d messages\n", r.TotalFailed)
	fmt.Printf("║  Total Data:      %.1f MB\n", float64(r.TotalBytes)/(1024*1024))
	fmt.Printf("║  Throughput:      %.0f msgs/sec\n", r.MsgsPerSecond)
	fmt.Printf("║  Bandwidth:       %.2f MB/sec\n", r.MBPerSecond)
	fmt.Printf("║  Avg Msg Size:    %d bytes\n", r.MessageSizes.Avg)
	fmt.Printf("║  Loss Rate:       %.2f%%\n", float64(r.TotalFailed)/float64(r.TotalSent+r.TotalFailed)*100)
	fmt.Println("╠══════════════════════════════════════════════════════════════╣")
	fmt.Println("║  PER-PORT BREAKDOWN:")
	for _, p := range r.PerPort {
		fmt.Printf("║    :%d  →  %d sent, %d failed, %.0f msgs/s, %.1f KB/s\n",
			p.Port, p.Sent, p.Failed, p.Rate, float64(p.BytesSent)/1024/r.Duration.Seconds())
	}
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
}

// --- API Helpers ---

func login(apiBase string) error {
	user := envOr("ADMIN_USER", "admin")
	pass := envOr("ADMIN_PASS", "arteria123")
	payload, _ := json.Marshal(map[string]string{"username": user, "password": pass})
	resp, err := http.Post(apiBase+"/api/v1/auth/login", "application/json", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if t, ok := result["token"].(string); ok {
		authToken = t
	}
	return nil
}

func getMetrics(apiBase string) map[string]interface{} {
	req, _ := http.NewRequest("GET", apiBase+"/api/v1/metrics", nil)
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	return result
}

func mllpSend(host string, port int, hl7 string) error {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", host, port), 5*time.Second)
	if err != nil {
		return err
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

// --- Utilities ---

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func parseDuration(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 60 * time.Second
	}
	return d
}

func parseInt(s string) int {
	var n int
	fmt.Sscanf(s, "%d", &n)
	if n == 0 {
		return 1
	}
	return n
}

func parsePorts(s string) []int {
	var ports []int
	for _, p := range bytes.Split([]byte(s), []byte(",")) {
		var port int
		fmt.Sscanf(string(p), "%d", &port)
		if port > 0 {
			ports = append(ports, port)
		}
	}
	return ports
}
