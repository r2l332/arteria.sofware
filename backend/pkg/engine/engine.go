package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"sync"
	"time"

	"github.com/gocql/gocql"
	"github.com/r2l332/arteria.app/backend/pkg/v8pool"
)

// MessageEnvelope is the data structure that flows through the filter chain.
type MessageEnvelope struct {
	MessageID       string            `json:"messageId"`
	MessageType     string            `json:"messageType"`
	TriggerEvent    string            `json:"triggerEvent"`
	SendingFacility string            `json:"sendingFacility"`
	PatientID       string            `json:"patientId"`
	RawPayload      string            `json:"rawPayload"`
	Properties      map[string]string `json:"properties"`
}

// FilterResult is returned by each filter in the chain.
type FilterResult struct {
	Action  string // "pass", "reject", "route_to"
	Reason  string
	RouteTO string // destination override for conditional routing
	Output  *MessageEnvelope
}

// Filter represents a single processing step in a route's filter chain.
type Filter struct {
	FilterID       gocql.UUID
	RouteID        gocql.UUID
	Name           string
	FilterType     string // "javascript", "conditional", "lookup", "duplicate_check"
	ExecutionOrder int
	JSScript       string
	ConfigJSON     string
	IsActive       bool
}

// Route represents a routing rule with its filter chain loaded.
type Route struct {
	RouteID          gocql.UUID
	Name             string
	SourceCommPoint  gocql.UUID
	DestCommPoint    gocql.UUID
	FanOutCPIDs      []gocql.UUID
	SourceTopic      string
	DestinationTopic string
	IsActive         bool
	Filters          []Filter
}

// Engine is the core processing engine that manages routes, filters, and V8 execution.
type Engine struct {
	v8Pool  *v8pool.Pool
	session *gocql.Session

	routesMu sync.RWMutex
	routes   []Route

	lookupMu sync.RWMutex
	lookups  map[string]map[string]string // table_name -> key -> value
}

// New creates a new processing engine.
func New(pool *v8pool.Pool, session *gocql.Session) *Engine {
	e := &Engine{
		v8Pool:  pool,
		session: session,
		lookups: make(map[string]map[string]string),
	}
	return e
}

// LoadConfig loads routes, filters, and lookup tables from ScyllaDB.
func (e *Engine) LoadConfig() error {
	routes, err := e.loadRoutes()
	if err != nil {
		return fmt.Errorf("load routes: %w", err)
	}

	for i, route := range routes {
		filters, err := e.loadFilters(route.RouteID)
		if err != nil {
			log.Printf("[ENGINE] warning: failed to load filters for route %s: %v", route.Name, err)
			continue
		}
		routes[i].Filters = filters
	}

	lookups, err := e.loadLookups()
	if err != nil {
		log.Printf("[ENGINE] warning: failed to load lookups: %v", err)
	}

	e.routesMu.Lock()
	e.routes = routes
	e.routesMu.Unlock()

	e.lookupMu.Lock()
	e.lookups = lookups
	e.lookupMu.Unlock()

	log.Printf("[ENGINE] loaded %d routes, %d lookup tables", len(routes), len(lookups))
	return nil
}

// StartConfigReloader periodically reloads config from ScyllaDB.
func (e *Engine) StartConfigReloader(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := e.LoadConfig(); err != nil {
					log.Printf("[ENGINE] config reload error: %v", err)
				}
			}
		}
	}()
}

// ProcessMessage runs a message through the matching route's filter chain.
// Returns: destination topic, dest comm point IDs (primary + fan-out), transformed payload, error
func (e *Engine) ProcessMessage(ctx context.Context, envelope *MessageEnvelope) (string, []string, string, error) {
	e.routesMu.RLock()
	routes := e.routes
	e.routesMu.RUnlock()

	matchKey := envelope.MessageType + "^" + envelope.TriggerEvent

	// Find matching route (most specific first, then catch-all)
	var matchedRoute *Route
	var catchAll *Route
	for i := range routes {
		if !routes[i].IsActive {
			continue
		}
		if routes[i].SourceTopic == matchKey {
			matchedRoute = &routes[i]
			break
		}
		if routes[i].SourceTopic == "*" {
			catchAll = &routes[i]
		}
	}

	if matchedRoute == nil {
		matchedRoute = catchAll
	}

	if matchedRoute == nil {
		// No route matched, pass through
		payloadBytes, _ := json.Marshal(envelope)
		return "default", nil, string(payloadBytes), nil
	}

	// Collect all destination CP IDs (primary + fan-out)
	destCPIDs := []string{matchedRoute.DestCommPoint.String()}
	for _, cpID := range matchedRoute.FanOutCPIDs {
		destCPIDs = append(destCPIDs, cpID.String())
	}

	// Execute filter chain
	result, err := e.executeFilterChain(ctx, matchedRoute, envelope)
	if err != nil {
		return "", nil, "", fmt.Errorf("filter chain error on route %s: %w", matchedRoute.Name, err)
	}

	if result.Action == "reject" {
		return "", nil, "", fmt.Errorf("message rejected by filter: %s", result.Reason)
	}

	destTopic := matchedRoute.DestinationTopic
	if result.Action == "route_to" && result.RouteTO != "" {
		destTopic = result.RouteTO
	}

	// If no filters modified the message, pass through the raw payload unchanged
	if len(matchedRoute.Filters) == 0 || !hasActiveFilters(matchedRoute.Filters) {
		return destTopic, destCPIDs, result.Output.RawPayload, nil
	}

	outputBytes, _ := json.Marshal(result.Output)
	return destTopic, destCPIDs, string(outputBytes), nil
}

// executeFilterChain runs all active filters for a route in order.
func (e *Engine) executeFilterChain(ctx context.Context, route *Route, envelope *MessageEnvelope) (*FilterResult, error) {
	current := envelope

	for _, filter := range route.Filters {
		if !filter.IsActive {
			continue
		}

		var result *FilterResult
		var err error

		switch filter.FilterType {
		case "javascript":
			result, err = e.executeJSFilter(ctx, &filter, current)
		case "conditional":
			result, err = e.executeConditionalFilter(ctx, &filter, current)
		case "lookup":
			result, err = e.executeLookupFilter(&filter, current)
		case "python", "bash", "powershell", "dotnet":
			result, err = e.executeScriptFilter(ctx, &filter, current)
		default:
			log.Printf("[ENGINE] unknown filter type %q, skipping", filter.FilterType)
			continue
		}

		if err != nil {
			return nil, fmt.Errorf("filter %q (order=%d): %w", filter.Name, filter.ExecutionOrder, err)
		}

		if result.Action == "reject" || result.Action == "route_to" {
			return result, nil
		}

		// Pass output to next filter
		if result.Output != nil {
			current = result.Output
		}
	}

	return &FilterResult{
		Action: "pass",
		Output: current,
	}, nil
}

// executeJSFilter runs a JavaScript transform filter via the V8 pool.
// Script must define: function transform(msg) { ... return msg; }
func (e *Engine) executeJSFilter(ctx context.Context, filter *Filter, envelope *MessageEnvelope) (*FilterResult, error) {
	payloadBytes, _ := json.Marshal(envelope)

	result := e.v8Pool.Execute(ctx, filter.JSScript, string(payloadBytes))
	if result.Error != nil {
		return nil, result.Error
	}

	var output MessageEnvelope
	if err := json.Unmarshal([]byte(result.Output), &output); err != nil {
		return nil, fmt.Errorf("unmarshal V8 output: %w", err)
	}

	return &FilterResult{
		Action: "pass",
		Output: &output,
	}, nil
}

// executeConditionalFilter runs a JS predicate that returns routing decisions.
// Script must define: function evaluate(msg) { return { action: "pass"|"reject"|"route_to", ... }; }
func (e *Engine) executeConditionalFilter(ctx context.Context, filter *Filter, envelope *MessageEnvelope) (*FilterResult, error) {
	payloadBytes, _ := json.Marshal(envelope)

	// Wrap script to call evaluate() instead of transform()
	script := filter.JSScript + "\nfunction transform(msg) { return evaluate(msg); }"

	result := e.v8Pool.Execute(ctx, script, string(payloadBytes))
	if result.Error != nil {
		return nil, result.Error
	}

	var decision struct {
		Action  string `json:"action"`
		Reason  string `json:"reason"`
		RouteTo string `json:"route_to"`
	}
	if err := json.Unmarshal([]byte(result.Output), &decision); err != nil {
		return nil, fmt.Errorf("unmarshal conditional result: %w", err)
	}

	return &FilterResult{
		Action:  decision.Action,
		Reason:  decision.Reason,
		RouteTO: decision.RouteTo,
		Output:  envelope, // Conditional filters don't modify the message
	}, nil
}

// executeLookupFilter enriches a message with data from a lookup table.
func (e *Engine) executeLookupFilter(filter *Filter, envelope *MessageEnvelope) (*FilterResult, error) {
	var config struct {
		TableName    string `json:"table_name"`
		LookupField  string `json:"lookup_field"`  // field on the message to use as key
		OutputField  string `json:"output_field"`   // property to set with the looked-up value
	}
	if err := json.Unmarshal([]byte(filter.ConfigJSON), &config); err != nil {
		return nil, fmt.Errorf("parse lookup config: %w", err)
	}

	e.lookupMu.RLock()
	table, ok := e.lookups[config.TableName]
	e.lookupMu.RUnlock()

	if !ok {
		return &FilterResult{Action: "pass", Output: envelope}, nil
	}

	// Get the key value from the message
	var lookupKey string
	switch config.LookupField {
	case "patientId":
		lookupKey = envelope.PatientID
	case "sendingFacility":
		lookupKey = envelope.SendingFacility
	case "messageType":
		lookupKey = envelope.MessageType
	default:
		lookupKey = envelope.Properties[config.LookupField]
	}

	if val, found := table[lookupKey]; found {
		if envelope.Properties == nil {
			envelope.Properties = make(map[string]string)
		}
		envelope.Properties[config.OutputField] = val
	}

	return &FilterResult{Action: "pass", Output: envelope}, nil
}

// --- Data loading helpers ---

func hasActiveFilters(filters []Filter) bool {
	for _, f := range filters {
		if f.IsActive {
			return true
		}
	}
	return false
}

func (e *Engine) loadRoutes() ([]Route, error) {
	var routes []Route
	iter := e.session.Query(`SELECT route_id, name, source_comm_point_id, dest_comm_point_id, source_topic, destination_topic, is_active, fan_out_cp_ids FROM arteria.routes`).Iter()

	var r Route
	for iter.Scan(&r.RouteID, &r.Name, &r.SourceCommPoint, &r.DestCommPoint, &r.SourceTopic, &r.DestinationTopic, &r.IsActive, &r.FanOutCPIDs) {
		routes = append(routes, r)
		r = Route{}
	}
	return routes, iter.Close()
}

func (e *Engine) loadFilters(routeID gocql.UUID) ([]Filter, error) {
	var filters []Filter
	iter := e.session.Query(`SELECT filter_id, route_id, name, filter_type, execution_order, js_script, config_json, is_active FROM arteria.filters WHERE route_id = ?`, routeID).Iter()

	var f Filter
	for iter.Scan(&f.FilterID, &f.RouteID, &f.Name, &f.FilterType, &f.ExecutionOrder, &f.JSScript, &f.ConfigJSON, &f.IsActive) {
		filters = append(filters, f)
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}

	sort.Slice(filters, func(i, j int) bool {
		return filters[i].ExecutionOrder < filters[j].ExecutionOrder
	})
	return filters, nil
}

func (e *Engine) loadLookups() (map[string]map[string]string, error) {
	lookups := make(map[string]map[string]string)

	// Load table names
	tableIter := e.session.Query(`SELECT table_id, name FROM arteria.lookup_tables`).Iter()
	var tableID gocql.UUID
	var tableName string
	tableMap := make(map[gocql.UUID]string) // id -> name

	for tableIter.Scan(&tableID, &tableName) {
		tableMap[tableID] = tableName
		lookups[tableName] = make(map[string]string)
	}
	if err := tableIter.Close(); err != nil {
		return nil, err
	}

	// Load entries for each table
	for id, name := range tableMap {
		entryIter := e.session.Query(`SELECT lookup_key, lookup_value FROM arteria.lookup_entries WHERE table_id = ?`, id).Iter()
		var key, value string
		for entryIter.Scan(&key, &value) {
			lookups[name][key] = value
		}
		entryIter.Close()
	}

	return lookups, nil
}

// GetRoutes returns a copy of the current routes (for the API).
func (e *Engine) GetRoutes() []Route {
	e.routesMu.RLock()
	defer e.routesMu.RUnlock()
	result := make([]Route, len(e.routes))
	copy(result, e.routes)
	return result
}

// executeScriptFilter runs a Python/Bash/PowerShell/.NET script as a subprocess.
// The message is passed as JSON on stdin, the transformed message is read from stdout.
func (e *Engine) executeScriptFilter(ctx context.Context, filter *Filter, envelope *MessageEnvelope) (*FilterResult, error) {
	// Determine the interpreter
	var cmd string
	var args []string
	switch filter.FilterType {
	case "python":
		cmd = "python3"
		args = []string{"-c", filter.JSScript}
	case "bash":
		cmd = "bash"
		args = []string{"-c", filter.JSScript}
	case "powershell":
		cmd = "pwsh"
		args = []string{"-Command", filter.JSScript}
	case "dotnet":
		cmd = "dotnet-script"
		args = []string{"eval", filter.JSScript}
	default:
		return nil, fmt.Errorf("unsupported script type: %s", filter.FilterType)
	}

	// Serialize the message envelope as JSON input
	inputBytes, _ := json.Marshal(envelope)

	// Create the command with timeout
	execCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()

	proc := exec.CommandContext(execCtx, cmd, args...)
	proc.Stdin = bytes.NewReader(inputBytes)

	// Capture stdout and stderr
	var stdout, stderr bytes.Buffer
	proc.Stdout = &stdout
	proc.Stderr = &stderr

	// Execute
	if err := proc.Run(); err != nil {
		if execCtx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("%s script timeout (500ms limit)", filter.FilterType)
		}
		return nil, fmt.Errorf("%s script error: %v (stderr: %s)", filter.FilterType, err, stderr.String())
	}

	// Parse output as JSON message envelope
	output := stdout.Bytes()
	if len(output) == 0 {
		// Script produced no output — pass through unchanged
		return &FilterResult{Action: "pass", Output: envelope}, nil
	}

	var result MessageEnvelope
	if err := json.Unmarshal(output, &result); err != nil {
		// Try to parse as a routing decision (like conditional filters)
		var decision struct {
			Action  string `json:"action"`
			Reason  string `json:"reason"`
			RouteTo string `json:"route_to"`
		}
		if json.Unmarshal(output, &decision) == nil && decision.Action != "" {
			return &FilterResult{
				Action:  decision.Action,
				Reason:  decision.Reason,
				RouteTO: decision.RouteTo,
				Output:  envelope,
			}, nil
		}
		return nil, fmt.Errorf("%s script output is not valid JSON: %v", filter.FilterType, err)
	}

	return &FilterResult{Action: "pass", Output: &result}, nil
}
