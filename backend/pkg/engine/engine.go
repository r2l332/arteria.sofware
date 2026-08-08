package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"text/template"
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

// FanOutEntry defines a conditional output CP in a fan-out configuration.
type FanOutEntry struct {
	CPID          string `json:"cp_id"`
	Name          string `json:"name,omitempty"`
	ConditionType string `json:"condition_type,omitempty"` // "python", "javascript", "bash", "" (unconditional)
	Condition     string `json:"condition,omitempty"`      // Script that returns "true"/"false" on stdout
}

// Route represents a routing rule with its filter chain loaded.
type Route struct {
	RouteID            gocql.UUID
	Name               string
	SourceCommPoint    gocql.UUID
	DestCommPoint      gocql.UUID
	FanOutCPIDs        []gocql.UUID   // Simple unconditional fan-out (legacy)
	FanOutConfig       []FanOutEntry  // Conditional fan-out
	FanOutConfigRaw    string         // Raw JSON from DB
	SourceTopic        string
	DestinationTopic   string
	IsActive           bool
	Filters            []Filter
	DefaultProperties  map[string]string // Injected into every message before the filter chain
	DefaultPropsRaw    string            // Raw JSON from DB
	NextRouteID        *gocql.UUID       // If set, forward to this route after completion
	OrgID              *gocql.UUID       // Owning organisation
}

// ConnectorConfig defines an outbound call made mid-filter-chain.
type ConnectorConfig struct {
	ConnectorType          string            `json:"connector_type"`           // "HTTP", "MLLP"
	URL                    string            `json:"url,omitempty"`            // HTTP endpoint
	Method                 string            `json:"method,omitempty"`         // GET, POST, PUT
	Headers                map[string]string `json:"headers,omitempty"`
	TimeoutMs              int               `json:"timeout_ms,omitempty"`     // default 5000
	Host                   string            `json:"host,omitempty"`           // MLLP host
	Port                   int               `json:"port,omitempty"`           // MLLP port
	BodyTemplate           string            `json:"body_template,omitempty"`  // Go template for HTTP body, {{.RawPayload}}, {{.Properties.key}}
	ResponseProperty       string            `json:"response_property"`        // Property key to store the response body
	ResponseStatusProperty string            `json:"response_status_property"` // Property key to store the status code / ACK code
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
// Returns: destination topic, dest comm point IDs, transformed rawPayload, full output envelope, error
func (e *Engine) ProcessMessage(ctx context.Context, envelope *MessageEnvelope) (string, []string, string, *MessageEnvelope, error) {
	return e.processMessageWithDepth(ctx, envelope, 0)
}

const maxChainDepth = 10

func (e *Engine) processMessageWithDepth(ctx context.Context, envelope *MessageEnvelope, depth int) (string, []string, string, *MessageEnvelope, error) {
	if depth >= maxChainDepth {
		return "", nil, "", nil, fmt.Errorf("route chain depth exceeded (max %d)", maxChainDepth)
	}

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
		return "default", nil, string(payloadBytes), envelope, nil
	}

	// Inject route default properties into the message
	if len(matchedRoute.DefaultProperties) > 0 {
		if envelope.Properties == nil {
			envelope.Properties = make(map[string]string)
		}
		for k, v := range matchedRoute.DefaultProperties {
			if _, exists := envelope.Properties[k]; !exists {
				envelope.Properties[k] = v
			}
		}
	}

	// Collect all destination CP IDs (primary + fan-out)
	destCPIDs := []string{matchedRoute.DestCommPoint.String()}
	for _, cpID := range matchedRoute.FanOutCPIDs {
		destCPIDs = append(destCPIDs, cpID.String())
	}

	// Evaluate conditional fan-out entries
	for _, entry := range matchedRoute.FanOutConfig {
		if entry.Condition == "" || entry.ConditionType == "" {
			// No condition — always include
			destCPIDs = append(destCPIDs, entry.CPID)
		} else {
			// Evaluate condition script
			if e.evaluateCondition(ctx, entry, envelope) {
				destCPIDs = append(destCPIDs, entry.CPID)
			}
		}
	}

	// Execute filter chain
	result, err := e.executeFilterChain(ctx, matchedRoute, envelope)
	if err != nil {
		return "", nil, "", nil, fmt.Errorf("filter chain error on route %s: %w", matchedRoute.Name, err)
	}

	if result.Action == "reject" {
		return "", nil, "", nil, fmt.Errorf("message rejected by filter: %s", result.Reason)
	}

	destTopic := matchedRoute.DestinationTopic
	if result.Action == "route_to" && result.RouteTO != "" {
		destTopic = result.RouteTO
	}

	// If no filters modified the message, pass through the raw payload unchanged
	if len(matchedRoute.Filters) == 0 || !hasActiveFilters(matchedRoute.Filters) {
		if matchedRoute.NextRouteID != nil {
			return e.chainToRoute(ctx, matchedRoute.NextRouteID, result.Output, depth)
		}
		return destTopic, destCPIDs, result.Output.RawPayload, result.Output, nil
	}

	// Route chaining: forward to next route if configured
	if matchedRoute.NextRouteID != nil {
		return e.chainToRoute(ctx, matchedRoute.NextRouteID, result.Output, depth)
	}

	// Filters ran — deliver the rawPayload, store full envelope for audit
	return destTopic, destCPIDs, result.Output.RawPayload, result.Output, nil
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
		case "connector":
			result, err = e.executeConnectorFilter(ctx, &filter, current)
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

// evaluateCondition runs a condition script and returns true if the message should be delivered.
func (e *Engine) evaluateCondition(ctx context.Context, entry FanOutEntry, envelope *MessageEnvelope) bool {
	var cmd string
	var args []string
	switch entry.ConditionType {
	case "python":
		cmd = "python3"
		args = []string{"-c", entry.Condition}
	case "javascript":
		cmd = "node"
		args = []string{"-e", entry.Condition}
	case "bash":
		cmd = "bash"
		args = []string{"-c", entry.Condition}
	case "dotnet":
		cmd = "dotnet-script"
		args = []string{"eval", entry.Condition}
	default:
		return true // Unknown type — deliver unconditionally
	}

	inputBytes, _ := json.Marshal(envelope)
	execCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	proc := exec.CommandContext(execCtx, cmd, args...)
	proc.Stdin = bytes.NewReader(inputBytes)

	var stdout bytes.Buffer
	proc.Stdout = &stdout

	if err := proc.Run(); err != nil {
		// Script error or non-zero exit = don't deliver
		return false
	}

	result := strings.TrimSpace(stdout.String())
	return result == "true" || result == "1" || result == "yes"
}

func (e *Engine) loadRoutes() ([]Route, error) {
	var routes []Route
	iter := e.session.Query(`SELECT route_id, name, source_comm_point_id, dest_comm_point_id, source_topic, destination_topic, is_active, fan_out_cp_ids, fan_out_config, default_properties, next_route_id, org_id FROM arteria.routes`).Iter()

	var r Route
	var nextRouteID *gocql.UUID
	var orgID *gocql.UUID
	for iter.Scan(&r.RouteID, &r.Name, &r.SourceCommPoint, &r.DestCommPoint, &r.SourceTopic, &r.DestinationTopic, &r.IsActive, &r.FanOutCPIDs, &r.FanOutConfigRaw, &r.DefaultPropsRaw, &nextRouteID, &orgID) {
		if r.FanOutConfigRaw != "" {
			json.Unmarshal([]byte(r.FanOutConfigRaw), &r.FanOutConfig)
		} else {
			r.FanOutConfig = nil
		}
		if r.DefaultPropsRaw != "" {
			json.Unmarshal([]byte(r.DefaultPropsRaw), &r.DefaultProperties)
		} else {
			r.DefaultProperties = nil
		}
		r.NextRouteID = nextRouteID
		r.OrgID = orgID
		routes = append(routes, r)
		r = Route{}
		nextRouteID = nil
		orgID = nil
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

// GetMatchedOrgID returns the org_id for a message type by finding the matching route.
func (e *Engine) GetMatchedOrgID(messageType, triggerEvent string) string {
	e.routesMu.RLock()
	defer e.routesMu.RUnlock()
	matchKey := messageType + "^" + triggerEvent
	for i := range e.routes {
		if !e.routes[i].IsActive { continue }
		if e.routes[i].SourceTopic == matchKey {
			if e.routes[i].OrgID != nil { return e.routes[i].OrgID.String() }
			return ""
		}
	}
	for i := range e.routes {
		if e.routes[i].IsActive && e.routes[i].SourceTopic == "*" {
			if e.routes[i].OrgID != nil { return e.routes[i].OrgID.String() }
			return ""
		}
	}
	return ""
}

// GetMatchedRouteInfo returns route_id, source_cp_id, org_id for a message type.
func (e *Engine) GetMatchedRouteInfo(messageType, triggerEvent string) (routeID, sourceCPID, orgID string) {
	e.routesMu.RLock()
	defer e.routesMu.RUnlock()
	matchKey := messageType + "^" + triggerEvent
	var matched *Route
	for i := range e.routes {
		if !e.routes[i].IsActive { continue }
		if e.routes[i].SourceTopic == matchKey { matched = &e.routes[i]; break }
	}
	if matched == nil {
		for i := range e.routes {
			if e.routes[i].IsActive && e.routes[i].SourceTopic == "*" { matched = &e.routes[i]; break }
		}
	}
	if matched == nil { return }
	routeID = matched.RouteID.String()
	sourceCPID = matched.SourceCommPoint.String()
	if matched.OrgID != nil { orgID = matched.OrgID.String() }
	return
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

	// Create the command with timeout (dotnet needs longer for JIT warmup)
	timeout := 2 * time.Second
	if filter.FilterType == "dotnet" {
		timeout = 10 * time.Second
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
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
			return nil, fmt.Errorf("%s script timeout (%v limit)", filter.FilterType, timeout)
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

	// Sync modified envelope fields back into rawPayload (HL7 MSH segment)
	syncEnvelopeToRawPayload(&result, envelope)

	return &FilterResult{Action: "pass", Output: &result}, nil
}

// syncEnvelopeToRawPayload updates HL7 MSH fields in RawPayload if the script
// modified envelope fields like sendingFacility but didn't touch rawPayload.
func syncEnvelopeToRawPayload(result, original *MessageEnvelope) {
	if result.RawPayload == "" || result.RawPayload == original.RawPayload {
		// Script didn't modify rawPayload directly — rebuild from envelope fields
		lines := strings.Split(result.RawPayload, "\r")
		if len(lines) == 0 || !strings.HasPrefix(lines[0], "MSH|") {
			return
		}
		fields := strings.Split(lines[0], "|")
		if len(fields) < 12 {
			return
		}
		changed := false
		if result.SendingFacility != original.SendingFacility && result.SendingFacility != "" {
			fields[3] = result.SendingFacility
			changed = true
		}
		if result.MessageType != original.MessageType && result.MessageType != "" {
			fields[8] = result.MessageType + "^" + result.TriggerEvent
			changed = true
		}
		if changed {
			lines[0] = strings.Join(fields, "|")
			result.RawPayload = strings.Join(lines, "\r")
		}
	}
}

// chainToRoute finds a route by ID and processes the message through it.
func (e *Engine) chainToRoute(ctx context.Context, routeID *gocql.UUID, envelope *MessageEnvelope, depth int) (string, []string, string, *MessageEnvelope, error) {
	e.routesMu.RLock()
	var target *Route
	for i := range e.routes {
		if e.routes[i].RouteID == *routeID && e.routes[i].IsActive {
			target = &e.routes[i]
			break
		}
	}
	e.routesMu.RUnlock()

	if target == nil {
		return "", nil, "", nil, fmt.Errorf("chained route %s not found or inactive", routeID.String())
	}

	// Inject the target route's default properties
	if len(target.DefaultProperties) > 0 {
		if envelope.Properties == nil {
			envelope.Properties = make(map[string]string)
		}
		for k, v := range target.DefaultProperties {
			if _, exists := envelope.Properties[k]; !exists {
				envelope.Properties[k] = v
			}
		}
	}

	result, err := e.executeFilterChain(ctx, target, envelope)
	if err != nil {
		return "", nil, "", nil, fmt.Errorf("chained route %s: %w", target.Name, err)
	}
	if result.Action == "reject" {
		return "", nil, "", nil, fmt.Errorf("chained route %s rejected: %s", target.Name, result.Reason)
	}

	destTopic := target.DestinationTopic
	if result.Action == "route_to" && result.RouteTO != "" {
		destTopic = result.RouteTO
	}

	destCPIDs := []string{target.DestCommPoint.String()}
	for _, cpID := range target.FanOutCPIDs {
		destCPIDs = append(destCPIDs, cpID.String())
	}

	// Continue the chain if the target also has a next route
	if target.NextRouteID != nil {
		return e.processMessageWithDepth(ctx, result.Output, depth+1)
	}

	return destTopic, destCPIDs, result.Output.RawPayload, result.Output, nil
}

// executeConnectorFilter makes an outbound call (HTTP/MLLP) mid-filter-chain
// and stores the response in message properties for subsequent filters.
func (e *Engine) executeConnectorFilter(ctx context.Context, filter *Filter, envelope *MessageEnvelope) (*FilterResult, error) {
	var cfg ConnectorConfig
	if err := json.Unmarshal([]byte(filter.ConfigJSON), &cfg); err != nil {
		return nil, fmt.Errorf("parse connector config: %w", err)
	}

	if envelope.Properties == nil {
		envelope.Properties = make(map[string]string)
	}

	timeout := 5 * time.Second
	if cfg.TimeoutMs > 0 {
		timeout = time.Duration(cfg.TimeoutMs) * time.Millisecond
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	switch strings.ToUpper(cfg.ConnectorType) {
	case "HTTP":
		return e.connectorHTTP(callCtx, &cfg, filter, envelope)
	case "MLLP":
		return e.connectorMLLP(callCtx, &cfg, filter, envelope)
	default:
		return nil, fmt.Errorf("unsupported connector type: %s", cfg.ConnectorType)
	}
}

func (e *Engine) connectorHTTP(ctx context.Context, cfg *ConnectorConfig, filter *Filter, envelope *MessageEnvelope) (*FilterResult, error) {
	// Build request body from template or use raw payload
	var body string
	if cfg.BodyTemplate != "" {
		tmpl, err := template.New("body").Parse(cfg.BodyTemplate)
		if err != nil {
			return nil, fmt.Errorf("parse body template: %w", err)
		}
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, envelope); err != nil {
			return nil, fmt.Errorf("execute body template: %w", err)
		}
		body = buf.String()
	} else {
		body = envelope.RawPayload
	}

	method := cfg.Method
	if method == "" {
		method = "POST"
	}

	req, err := http.NewRequestWithContext(ctx, method, cfg.URL, strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build HTTP request: %w", err)
	}
	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "text/plain")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		envelope.Properties[cfg.ResponseProperty] = ""
		envelope.Properties[cfg.ResponseStatusProperty] = "ERROR"
		envelope.Properties["_connector_error"] = err.Error()
		return &FilterResult{Action: "pass", Output: envelope}, nil
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB max
	if cfg.ResponseProperty != "" {
		envelope.Properties[cfg.ResponseProperty] = string(respBody)
	}
	if cfg.ResponseStatusProperty != "" {
		envelope.Properties[cfg.ResponseStatusProperty] = fmt.Sprintf("%d", resp.StatusCode)
	}

	return &FilterResult{Action: "pass", Output: envelope}, nil
}

func (e *Engine) connectorMLLP(ctx context.Context, cfg *ConnectorConfig, filter *Filter, envelope *MessageEnvelope) (*FilterResult, error) {
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		envelope.Properties[cfg.ResponseProperty] = ""
		envelope.Properties[cfg.ResponseStatusProperty] = "ERROR"
		envelope.Properties["_connector_error"] = err.Error()
		return &FilterResult{Action: "pass", Output: envelope}, nil
	}
	defer conn.Close()

	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	}

	// Send MLLP-framed message: 0x0B + payload + 0x1C + 0x0D
	var frame bytes.Buffer
	frame.WriteByte(0x0B)
	frame.WriteString(envelope.RawPayload)
	frame.WriteByte(0x1C)
	frame.WriteByte(0x0D)
	if _, err := conn.Write(frame.Bytes()); err != nil {
		envelope.Properties[cfg.ResponseProperty] = ""
		envelope.Properties[cfg.ResponseStatusProperty] = "ERROR"
		envelope.Properties["_connector_error"] = err.Error()
		return &FilterResult{Action: "pass", Output: envelope}, nil
	}

	// Read MLLP-framed response (ACK)
	var resp bytes.Buffer
	buf := make([]byte, 4096)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			resp.Write(buf[:n])
			// Check for MLLP end-of-message
			if bytes.Contains(resp.Bytes(), []byte{0x1C, 0x0D}) {
				break
			}
		}
		if err != nil {
			break
		}
	}

	// Strip MLLP framing from response
	ack := resp.Bytes()
	if len(ack) > 0 && ack[0] == 0x0B {
		ack = ack[1:]
	}
	if idx := bytes.Index(ack, []byte{0x1C}); idx >= 0 {
		ack = ack[:idx]
	}

	ackStr := string(ack)
	if cfg.ResponseProperty != "" {
		envelope.Properties[cfg.ResponseProperty] = ackStr
	}

	// Extract ACK code from MSA segment (MSA|AA|..., MSA|AE|..., MSA|AR|...)
	ackCode := "UNKNOWN"
	for _, line := range strings.Split(ackStr, "\r") {
		if strings.HasPrefix(line, "MSA|") {
			parts := strings.Split(line, "|")
			if len(parts) >= 2 {
				ackCode = parts[1]
			}
			break
		}
	}
	if cfg.ResponseStatusProperty != "" {
		envelope.Properties[cfg.ResponseStatusProperty] = ackCode
	}

	return &FilterResult{Action: "pass", Output: envelope}, nil
}
