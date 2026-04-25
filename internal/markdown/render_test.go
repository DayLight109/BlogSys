package markdown

import (
	"strings"
	"testing"
)

func TestRenderBasicBold(t *testing.T) {
	h, err := Render("**hello**")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(h, "<strong>hello</strong>") {
		t.Errorf("expected <strong>hello</strong>, got %q", h)
	}
}

func TestSanitizeScriptTag(t *testing.T) {
	h, err := Render("hi <script>alert(1)</script> bye")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(strings.ToLower(h), "<script") {
		t.Errorf("script tag should be stripped, got %q", h)
	}
}

func TestSanitizeOnerrorAttribute(t *testing.T) {
	h, err := Render(`<img src="x" onerror="alert(1)">`)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(strings.ToLower(h), "onerror") {
		t.Errorf("onerror should be stripped, got %q", h)
	}
}

func TestSanitizeJavascriptURL(t *testing.T) {
	h, err := Render("[click](javascript:alert(1))")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(strings.ToLower(h), "javascript:") {
		t.Errorf("javascript: URL should be stripped, got %q", h)
	}
}

func TestPreserveCodeLanguageClass(t *testing.T) {
	src := "```go\nfmt.Println(\"hi\")\n```\n"
	h, err := Render(src)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(h, `class="language-go"`) {
		t.Errorf("expected language-go class to survive, got %q", h)
	}
}

func TestPreserveHeadingID(t *testing.T) {
	// goldmark AutoHeadingID 会给标题自动加 id。
	h, err := Render("## Hello World")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(h, `<h2 id=`) {
		t.Errorf("expected <h2 id=...>, got %q", h)
	}
}

func TestAllowMailtoLink(t *testing.T) {
	h, err := Render("[me](mailto:a@b.c)")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(h, "mailto:a@b.c") {
		t.Errorf("mailto should survive, got %q", h)
	}
}

func TestRenderTable(t *testing.T) {
	src := "| a | b |\n|---|---|\n| 1 | 2 |\n"
	h, err := Render(src)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(h, "<table>") {
		t.Errorf("GFM table should render, got %q", h)
	}
}
