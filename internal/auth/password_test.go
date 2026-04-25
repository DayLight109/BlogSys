package auth

import (
	"strings"
	"testing"
)

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := HashPassword("hunter2")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if !strings.HasPrefix(hash, "$2a$") && !strings.HasPrefix(hash, "$2b$") {
		t.Fatalf("expected bcrypt prefix, got %q", hash)
	}
	if !VerifyPassword(hash, "hunter2") {
		t.Errorf("correct password should verify")
	}
	if VerifyPassword(hash, "wrong") {
		t.Errorf("wrong password should not verify")
	}
}

func TestVerifyPasswordEmptyHash(t *testing.T) {
	// 空 hash 不应 panic — VerifyPassword 应安全返回 false。
	if VerifyPassword("", "anything") {
		t.Errorf("empty hash should not verify any password")
	}
}

func TestDummyCompareDoesNotPanic(t *testing.T) {
	// DummyCompare 用于 timing equalisation;不应 panic 或返回错误。
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("DummyCompare panicked: %v", r)
		}
	}()
	DummyCompare()
}

func TestHashIsSaltedDistinct(t *testing.T) {
	// 同一密码两次 hash 应得到不同结果(bcrypt 内置随机 salt)。
	a, _ := HashPassword("same-password")
	b, _ := HashPassword("same-password")
	if a == b {
		t.Errorf("two hashes of same password should differ due to salt")
	}
	// 但都能验证通过。
	if !VerifyPassword(a, "same-password") || !VerifyPassword(b, "same-password") {
		t.Errorf("both hashes should verify the same plaintext")
	}
}
