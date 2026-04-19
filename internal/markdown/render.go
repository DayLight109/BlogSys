package markdown

import (
	"bytes"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
)

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

func Render(source string) (string, error) {
	var buf bytes.Buffer
	if err := md.Convert([]byte(source), &buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}
