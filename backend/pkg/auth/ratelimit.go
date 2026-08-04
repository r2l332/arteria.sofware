package auth

import (
	"sync"
	"time"
)

// RateLimiter provides brute-force protection for login attempts.
type RateLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
	maxAttempts int
	window      time.Duration
	lockout     time.Duration
}

// NewRateLimiter creates a rate limiter.
// Default: 5 attempts per 5 minutes, 15 minute lockout.
func NewRateLimiter(maxAttempts int, window, lockout time.Duration) *RateLimiter {
	rl := &RateLimiter{
		attempts:    make(map[string][]time.Time),
		maxAttempts: maxAttempts,
		window:      window,
		lockout:     lockout,
	}
	// Cleanup goroutine
	go func() {
		for {
			time.Sleep(5 * time.Minute)
			rl.cleanup()
		}
	}()
	return rl
}

// Allow checks if a login attempt is allowed for the given key (IP or username).
func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	attempts := rl.attempts[key]

	// Remove expired attempts
	valid := attempts[:0]
	for _, t := range attempts {
		if now.Sub(t) < rl.lockout {
			valid = append(valid, t)
		}
	}
	rl.attempts[key] = valid

	// Check if locked out
	recentCount := 0
	for _, t := range valid {
		if now.Sub(t) < rl.window {
			recentCount++
		}
	}

	return recentCount < rl.maxAttempts
}

// Record records a failed login attempt.
func (rl *RateLimiter) Record(key string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.attempts[key] = append(rl.attempts[key], time.Now())
}

// Reset clears attempts for a key (on successful login).
func (rl *RateLimiter) Reset(key string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.attempts, key)
}

func (rl *RateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	cutoff := time.Now().Add(-rl.lockout)
	for key, attempts := range rl.attempts {
		valid := attempts[:0]
		for _, t := range attempts {
			if t.After(cutoff) {
				valid = append(valid, t)
			}
		}
		if len(valid) == 0 {
			delete(rl.attempts, key)
		} else {
			rl.attempts[key] = valid
		}
	}
}
