package markdown

import (
	"bytes"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
)

// goldmark is configured with WithUnsafe() so that authored HTML in Markdown
// (e.g. <kbd>, a styled <figure>) survives conversion — we then run everything
// through a bluemonday policy that strips <script>, event handlers, and other
// XSS vectors. This keeps the expressive surface of Markdown without trusting
// the raw HTML pass-through.
var md = goldmark.New(
	goldmark.WithExtensions(
		extension.GFM,
		extension.Strikethrough,
		extension.Linkify,
		extension.TaskList,
		extension.Table,
	),
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	goldmark.WithRendererOptions(html.WithUnsafe()),
)

// sanitizer is built once and reused. UGCPolicy allows the common blog tags
// (p, a, code, pre, headings, lists, blockquote, img, tables, …) while
// forbidding <script>, on* handlers, <iframe>, <object>, data:/javascript:
// URLs, and similar classics. We extend it with a handful of attributes our
// renderer actually produces:
//   - class on code/pre/span/div so Shiki/language-* class names survive
//   - id on headings so the TOC anchor links resolve
//   - loading="lazy" on images so the browser respects our hint
var sanitizer = func() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	p.AllowAttrs("class").OnElements("code", "pre", "span", "div", "figure")
	p.AllowAttrs("id").OnElements("h1", "h2", "h3", "h4", "h5", "h6")
	p.AllowAttrs("loading").Matching(bluemonday.SpaceSeparatedTokens).OnElements("img")
	// Allow fragment + http(s) link targets only — no javascript:/data:/vbscript:.
	p.AllowURLSchemes("http", "https", "mailto", "tel")
	p.RequireNoFollowOnLinks(false) // don't add nofollow to every link; editorial choice
	p.AllowRelativeURLs(true)
	return p
}()

func Render(source string) (string, error) {
	var buf bytes.Buffer
	if err := md.Convert([]byte(source), &buf); err != nil {
		return "", err
	}
	return sanitizer.Sanitize(buf.String()), nil
}
