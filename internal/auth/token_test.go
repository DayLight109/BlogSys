package auth

import (
	"testing"
	"time"
)

func newTestManager() *TokenManager {
	return NewTokenManager("test-secret-32-bytes-aaaaaaaaaaaaaa", 30*time.Minute, 14*24*time.Hour)
}

func TestIssueAndParseAccess(t *testing.T) {
	tm := newTestManager()
	tok, exp, err := tm.Issue(42, "alice", "admin", TypeAccess)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if exp.Before(time.Now()) {
		t.Errorf("expiry should be in the future")
	}
	claims, err := tm.Parse(tok)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if claims.UserID != 42 || claims.Username != "alice" || claims.Role != "admin" || claims.Type != TypeAccess {
		t.Errorf("claims mismatch: %+v", claims)
	}
}

func TestIssueRefreshHasJTI(t *testing.T) {
	tm := newTestManager()
	tok, _, jti, err := tm.IssueWithID(1, "u", "admin", TypeRefresh)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if jti == "" {
		t.Fatalf("jti should be non-empty")
	}
	claims, err := tm.Parse(tok)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if claims.ID != jti {
		t.Errorf("parsed jti %q != issued %q", claims.ID, jti)
	}
}

func TestParseExpired(t *testing.T) {
	// TTL=1ns:Issue 完立刻就过期。
	tm := NewTokenManager("test-secret-32-bytes-aaaaaaaaaaaaaa", time.Nanosecond, time.Nanosecond)
	tok, _, err := tm.Issue(1, "u", "admin", TypeAccess)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, err := tm.Parse(tok); err == nil {
		t.Errorf("expired token should fail parse")
	}
}

func TestParseInvalidSignature(t *testing.T) {
	tm1 := NewTokenManager("secret-one-32-bytes-aaaaaaaaaaaa", time.Hour, time.Hour)
	tm2 := NewTokenManager("secret-two-32-bytes-bbbbbbbbbbbb", time.Hour, time.Hour)
	tok, _, err := tm1.Issue(1, "u", "admin", TypeAccess)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := tm2.Parse(tok); err == nil {
		t.Errorf("token signed by tm1 should not parse with tm2's secret")
	}
}

func TestIssueUnknownTokenType(t *testing.T) {
	tm := newTestManager()
	if _, _, err := tm.Issue(1, "u", "admin", "garbage"); err == nil {
		t.Errorf("unknown token type should error")
	}
}

func TestParseGarbage(t *testing.T) {
	tm := newTestManager()
	if _, err := tm.Parse("not-a-token"); err == nil {
		t.Errorf("garbage string should fail parse")
	}
}
