package v8pool

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	v8 "rogchap.com/v8go"
)

// Pool manages a set of pre-warmed V8 Isolates for executing user JS transforms.
type Pool struct {
	isolates chan *v8.Isolate
	size     int
	timeout  time.Duration
}

// Config holds pool configuration.
type Config struct {
	PoolSize int
	Timeout  time.Duration
}

// DefaultConfig returns sensible defaults for the V8 pool.
func DefaultConfig() Config {
	return Config{
		PoolSize: 8,
		Timeout:  50 * time.Millisecond,
	}
}

// New creates a pool of pre-warmed V8 Isolates.
func New(cfg Config) (*Pool, error) {
	p := &Pool{
		isolates: make(chan *v8.Isolate, cfg.PoolSize),
		size:     cfg.PoolSize,
		timeout:  cfg.Timeout,
	}

	for i := 0; i < cfg.PoolSize; i++ {
		iso := v8.NewIsolate()
		p.isolates <- iso
	}

	return p, nil
}

// ExecResult holds the output of a JS transform execution.
type ExecResult struct {
	Output string
	Error  error
}

// Execute runs a user's JS transform script against a JSON payload.
// The script must define: function transform(msg) { ... return msg; }
// The payload is injected as a JSON string, parsed in JS, and the result is serialized back.
func (p *Pool) Execute(ctx context.Context, script string, payload string) ExecResult {
	// Acquire an isolate from the pool
	var iso *v8.Isolate
	select {
	case iso = <-p.isolates:
	case <-ctx.Done():
		return ExecResult{Error: ctx.Err()}
	}
	defer func() { p.isolates <- iso }()

	// Create a fresh context per execution (cheap, isolates are expensive)
	v8ctx := v8.NewContext(iso)
	defer v8ctx.Close()

	// Build the full script: inject payload, define transform, execute
	fullScript := fmt.Sprintf(`
		var __input = JSON.parse(%s);
		%s
		JSON.stringify(transform(__input));
	`, jsonQuote(payload), script)

	// Execute with timeout
	resultCh := make(chan ExecResult, 1)
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		val, err := v8ctx.RunScript(fullScript, "transform.js")
		if err != nil {
			resultCh <- ExecResult{Error: fmt.Errorf("v8 exec: %w", err)}
			return
		}
		resultCh <- ExecResult{Output: val.String()}
	}()

	timeoutCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	select {
	case result := <-resultCh:
		return result
	case <-timeoutCtx.Done():
		iso.TerminateExecution()
		wg.Wait()
		return ExecResult{Error: fmt.Errorf("v8 execution timeout (%v)", p.timeout)}
	}
}

// Close disposes of all isolates in the pool.
func (p *Pool) Close() {
	for i := 0; i < p.size; i++ {
		iso := <-p.isolates
		iso.Dispose()
	}
}

// jsonQuote wraps a string as a valid JSON string literal for injection into JS.
func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
