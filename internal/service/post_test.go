package service

import (
	"strings"
	"testing"
)

func TestNormalizeSlugASCII(t *testing.T) {
	if got := normalizeSlug("Hello World", ""); got != "hello-world" {
		t.Errorf("got %q, want hello-world", got)
	}
}

func TestNormalizeSlugMultipleSpaces(t *testing.T) {
	if got := normalizeSlug("a   b", ""); got != "a-b" {
		t.Errorf("got %q, want a-b", got)
	}
}

func TestNormalizeSlugCollapseDashes(t *testing.T) {
	if got := normalizeSlug("a---b", ""); got != "a-b" {
		t.Errorf("got %q, want a-b", got)
	}
}

func TestNormalizeSlugChinesePreserved(t *testing.T) {
	// unicode.IsLetter 对中文也返回 true,所以中文字符应该保留。
	got := normalizeSlug("你好 World", "")
	if got != "你好-world" {
		t.Errorf("got %q, want 你好-world", got)
	}
}

func TestNormalizeSlugFallbackToTitle(t *testing.T) {
	if got := normalizeSlug("", "My Post"); got != "my-post" {
		t.Errorf("got %q, want my-post", got)
	}
}

func TestNormalizeSlugEmptyFallbackPrefix(t *testing.T) {
	// 没 slug 也没 title 时,降级为 "post-<时间戳>"。
	got := normalizeSlug("", "")
	if !strings.HasPrefix(got, "post-") {
		t.Errorf("expected post-... fallback, got %q", got)
	}
}

func TestNormalizeSlugTrimDashes(t *testing.T) {
	if got := normalizeSlug("---hi---", ""); got != "hi" {
		t.Errorf("got %q, want hi", got)
	}
}

func TestNormalizeSlugTruncated(t *testing.T) {
	long := strings.Repeat("a", 300)
	got := normalizeSlug(long, "")
	if len(got) > 200 {
		t.Errorf("len(got)=%d, want <=200", len(got))
	}
}

func TestNormalizeSlugStripsPunctuation(t *testing.T) {
	if got := normalizeSlug("Hello, World!", ""); got != "hello-world" {
		t.Errorf("got %q, want hello-world", got)
	}
}
