package server

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestPairLimiterBlocksAfterRepeatedFailures(t *testing.T) {
	limiter := newPairLimiter()
	now := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)

	for i := 1; i < pairMaxFailures; i++ {
		if _, blocked := limiter.recordFailure("192.0.2.10", now.Add(time.Duration(i)*time.Second)); blocked {
			t.Fatalf("failure %d blocked too early", i)
		}
	}

	blockedUntil, blocked := limiter.recordFailure("192.0.2.10", now.Add(pairMaxFailures*time.Second))
	if !blocked {
		t.Fatal("expected limiter to block after max failures")
	}
	if want := now.Add(pairMaxFailures * time.Second).Add(pairBlockDuration); !blockedUntil.Equal(want) {
		t.Fatalf("unexpected blockedUntil: got %s want %s", blockedUntil, want)
	}

	if _, blocked := limiter.blocked("192.0.2.10", blockedUntil.Add(time.Second)); blocked {
		t.Fatal("expected block to expire")
	}
}

func TestPairLimiterResetsOnSuccess(t *testing.T) {
	limiter := newPairLimiter()
	now := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)

	for i := 0; i < pairMaxFailures-1; i++ {
		limiter.recordFailure("192.0.2.10", now.Add(time.Duration(i)*time.Second))
	}
	limiter.recordSuccess("192.0.2.10")

	if _, blocked := limiter.recordFailure("192.0.2.10", now.Add(time.Minute)); blocked {
		t.Fatal("expected success to reset previous failures")
	}
}

func TestPairClientKeyUsesRemoteHost(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/pair", nil)
	req.RemoteAddr = "192.0.2.10:45678"

	if got := pairClientKey(req); got != "192.0.2.10" {
		t.Fatalf("unexpected client key: %q", got)
	}
}
