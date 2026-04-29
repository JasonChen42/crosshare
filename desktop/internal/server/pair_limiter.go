package server

import (
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	pairMaxFailures    = 5
	pairFailureWindow  = 5 * time.Minute
	pairBlockDuration  = 10 * time.Minute
	pairAttemptMaxIdle = 30 * time.Minute
)

type pairAttempt struct {
	failures     int
	windowStart  time.Time
	blockedUntil time.Time
	lastSeen     time.Time
}

type pairLimiter struct {
	mu       sync.Mutex
	attempts map[string]pairAttempt
}

func newPairLimiter() *pairLimiter {
	return &pairLimiter{attempts: make(map[string]pairAttempt)}
}

func (l *pairLimiter) blocked(key string, now time.Time) (time.Time, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.pruneLocked(now)
	attempt, ok := l.attempts[key]
	if !ok || !attempt.blockedUntil.After(now) {
		return time.Time{}, false
	}
	return attempt.blockedUntil, true
}

func (l *pairLimiter) recordFailure(key string, now time.Time) (time.Time, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.pruneLocked(now)
	attempt := l.attempts[key]
	if attempt.windowStart.IsZero() || now.Sub(attempt.windowStart) > pairFailureWindow {
		attempt.failures = 0
		attempt.windowStart = now
	}

	attempt.failures++
	attempt.lastSeen = now
	if attempt.failures >= pairMaxFailures {
		attempt.blockedUntil = now.Add(pairBlockDuration)
	}
	l.attempts[key] = attempt

	if attempt.blockedUntil.After(now) {
		return attempt.blockedUntil, true
	}
	return time.Time{}, false
}

func (l *pairLimiter) recordSuccess(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}

func (l *pairLimiter) pruneLocked(now time.Time) {
	for key, attempt := range l.attempts {
		if attempt.blockedUntil.After(now) {
			continue
		}
		if now.Sub(attempt.lastSeen) > pairAttemptMaxIdle {
			delete(l.attempts, key)
		}
	}
}

func pairClientKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if host == "" {
		return "unknown"
	}
	return host
}

func writeRetryAfter(w http.ResponseWriter, blockedUntil, now time.Time) {
	seconds := int(time.Until(blockedUntil).Seconds())
	if !now.IsZero() {
		seconds = int(blockedUntil.Sub(now).Seconds())
	}
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
}
