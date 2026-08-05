package auth

import (
	"sync"
	"time"
)

// ActiveSession tracks a user's current session state.
type ActiveSession struct {
	UserID     string    `json:"user_id"`
	Username   string    `json:"username"`
	Role       string    `json:"role"`
	LastActive time.Time `json:"last_active"`
	IP         string    `json:"ip"`
	UserAgent  string    `json:"user_agent"`
}

// SessionTracker maintains a list of active user sessions.
type SessionTracker struct {
	mu       sync.RWMutex
	sessions map[string]*ActiveSession // keyed by user_id
	timeout  time.Duration
}

// NewSessionTracker creates a session tracker with the given idle timeout.
func NewSessionTracker(timeout time.Duration) *SessionTracker {
	st := &SessionTracker{
		sessions: make(map[string]*ActiveSession),
		timeout:  timeout,
	}
	// Cleanup goroutine
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			st.cleanup()
		}
	}()
	return st
}

// Touch records activity for a user (called on every authenticated request).
func (st *SessionTracker) Touch(userID, username, role, ip, userAgent string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.sessions[userID] = &ActiveSession{
		UserID:     userID,
		Username:   username,
		Role:       role,
		LastActive: time.Now(),
		IP:         ip,
		UserAgent:  userAgent,
	}
}

// Online returns all sessions active within the timeout window.
func (st *SessionTracker) Online() []ActiveSession {
	st.mu.RLock()
	defer st.mu.RUnlock()
	cutoff := time.Now().Add(-st.timeout)
	var result []ActiveSession
	for _, s := range st.sessions {
		if s.LastActive.After(cutoff) {
			result = append(result, *s)
		}
	}
	return result
}

// Count returns the number of currently online users.
func (st *SessionTracker) Count() int {
	return len(st.Online())
}

func (st *SessionTracker) cleanup() {
	st.mu.Lock()
	defer st.mu.Unlock()
	cutoff := time.Now().Add(-st.timeout)
	for id, s := range st.sessions {
		if s.LastActive.Before(cutoff) {
			delete(st.sessions, id)
		}
	}
}
