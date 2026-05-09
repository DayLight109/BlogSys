package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

var (
	ErrChatNotConfigured = errors.New("chat ai is not configured")
	ErrChatBadRequest    = errors.New("invalid chat request")
)

const defaultChatSystemPrompt = `You are Kiri AI, a calm and practical assistant for a personal blog.

Identity rules:
- Your name is Kiri AI.
- If the user asks who you are, what model you are, who made you, whether you are ChatGPT, or any other identity question, answer that you are Kiri AI.
- Do not identify yourself as ChatGPT, OpenAI, GPT, Claude, Gemini, or any provider/model name.
- If provider/model details are relevant, say only that Kiri AI is the assistant in this blog system.

Help with writing, coding, planning, and editing. Be clear, specific, and concise.

When the user attaches a file or image, read its contents carefully before responding. Files arrive either as fenced text in the prompt, or — for images — as inline image_url parts.`

// ChatMessage carries a JSON content field that can be either a plain string
// (the simple text case) or an array of OpenAI-style content parts
// (multimodal: text + image_url). We use RawMessage so we can pass either form
// through to the upstream provider unchanged.
type ChatMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// contentPart mirrors the OpenAI multimodal shape for validation only.
type contentPart struct {
	Type     string        `json:"type"`
	Text     string        `json:"text,omitempty"`
	ImageURL *contentImage `json:"image_url,omitempty"`
}

type contentImage struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

const (
	maxTextPartLen     = 20000
	maxImageURLLen     = 8 * 1024 * 1024 // 8 MB cap on a single image data URL
	maxPartsPerMessage = 12
	maxMessages        = 50
)

type ChatService struct {
	apiKey         string
	baseURL        string
	model          string
	allowedModels  map[string]struct{} // empty → no model switching allowed
	webSearchTool  string              // legacy Responses-API tool name; unused on the chat-completions path
	webSearchModel string              // search-preview model swapped in when WebSearch is requested
	client         *http.Client
}

// ChatOptions are per-request knobs the handler builds from the public JSON
// fields and forwards to Stream/Complete. Zero-values are safe defaults.
type ChatOptions struct {
	Model        string // empty → use s.Model(); non-empty MUST be in allowedModels, else silently falls back
	WebSearch    bool
	DeepThinking bool
}

type ChatUpstreamError struct {
	StatusCode int
	Body       string
}

func (e *ChatUpstreamError) Error() string {
	if e == nil {
		return "chat upstream error"
	}
	return fmt.Sprintf("chat upstream error: status=%d body=%s", e.StatusCode, e.Body)
}

func NewChatService(apiKey, baseURL, model string, allowedModels []string, webSearchTool, webSearchModel string) *ChatService {
	set := make(map[string]struct{}, len(allowedModels))
	for _, m := range allowedModels {
		m = strings.TrimSpace(m)
		if m != "" {
			set[m] = struct{}{}
		}
	}
	return &ChatService{
		apiKey:         strings.TrimSpace(apiKey),
		baseURL:        strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		model:          strings.TrimSpace(model),
		allowedModels:  set,
		webSearchTool:  strings.TrimSpace(webSearchTool),
		webSearchModel: strings.TrimSpace(webSearchModel),
		client: &http.Client{
			Timeout: 180 * time.Second,
		},
	}
}

func (s *ChatService) Model() string {
	if s.model == "" {
		return "gpt-4o-mini"
	}
	return s.model
}

func (s *ChatService) BaseURL() string  { return s.baseURL }
func (s *ChatService) Configured() bool { return s.apiKey != "" && s.baseURL != "" && s.Model() != "" }
func (s *ChatService) APIKeySet() bool  { return s.apiKey != "" }

// AllowedModels returns the list of model ids the client is permitted to
// pick. When the operator hasn't configured an allow-list, the default model
// is returned alone so the popover can still show the active model name.
func (s *ChatService) AllowedModels() []string {
	if len(s.allowedModels) == 0 {
		return []string{s.Model()}
	}
	out := make([]string, 0, len(s.allowedModels))
	for k := range s.allowedModels {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

type chatTool struct {
	Type string `json:"type"`
}

// webSearchOptions is the (optional) knob OpenAI's search-preview chat models
// take in lieu of a tools array. Empty struct = "search with defaults" — the
// model always issues a search per request. We don't expose any fields yet
// (user_location / search_context_size could come later); using a pointer so
// that "field absent" and "field present but empty" remain distinguishable
// on the JSON wire. The presence of this field is what flips the behavior
// on the chat-completions endpoint.
type webSearchOptions struct{}

type chatCompletionRequest struct {
	Model            string            `json:"model"`
	Messages         []ChatMessage     `json:"messages"`
	Temperature      *float64          `json:"temperature,omitempty"`
	Stream           bool              `json:"stream"`
	Tools            []chatTool        `json:"tools,omitempty"`
	ReasoningEffort  string            `json:"reasoning_effort,omitempty"`
	WebSearchOptions *webSearchOptions `json:"web_search_options,omitempty"`
}

// buildRequest assembles the upstream JSON body from already-normalized
// messages and the per-request options.
//
// Behavior:
//   - When opts.Model is set and present in the allow-list, it overrides the
//     default model. An unknown model silently falls back to the default —
//     this protects users whose stored preference is no longer in the
//     allow-list after an admin tightened the list.
//   - WebSearch swaps the model out for a search-preview variant
//     (gpt-4o-search-preview / gpt-4o-mini-search-preview / gpt-5-search-api)
//     and attaches an empty `web_search_options:{}` knob. The legacy
//     `tools:[{"type":"web_search_preview"}]` shape is NOT used here — that
//     belongs to the Responses API, and Chat Completions silently ignores
//     unknown tool types, which is how we ended up with the "model says it
//     can't browse" failure mode in the first place. If
//     OPENAI_WEB_SEARCH_MODEL isn't configured the request falls back to the
//     plain chat path with no search.
//   - Search-preview models reject sampling knobs (temperature,
//     reasoning_effort, top_p). Drop them on the search path. The
//     non-search path keeps the original temperature/reasoning_effort
//     behavior for normal models.
//   - DeepThinking sends reasoning_effort=high; non-reasoning models on the
//     OpenAI Chat Completions endpoint ignore the field, so it's safe to send
//     to any upstream. We also lower temperature to 0.3 so the answer comes
//     out steadier when the model does honor the effort hint.
func (s *ChatService) buildRequest(messages []ChatMessage, opts ChatOptions, stream bool) chatCompletionRequest {
	model := s.Model()
	if opts.Model != "" {
		if _, ok := s.allowedModels[opts.Model]; ok {
			model = opts.Model
		}
	}

	body := chatCompletionRequest{
		Model:    model,
		Messages: messages,
		Stream:   stream,
	}
	// something very weird in here,don't know what reason.
	// Web-search path: swap to search-preview model and attach
	// web_search_options. Skip sampling knobs entirely — the search models
	// 400 on temperature / reasoning_effort.
	if opts.WebSearch && s.webSearchModel != "" {
		body.Model = s.webSearchModel
		body.WebSearchOptions = &webSearchOptions{}
		return body
	}

	// Standard path — keep the existing temperature / reasoning_effort
	// behavior. Use a pointer for temperature so DeepThinking-off
	// requests still send 0.7 explicitly (unchanged behavior).
	if opts.DeepThinking {
		body.ReasoningEffort = "high"
		t := 0.3
		body.Temperature = &t
	} else {
		t := 0.7
		body.Temperature = &t
	}
	return body
}

type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

type chatCompletionChunk struct {
	Choices []struct {
		Delta struct {
			Content   string         `json:"content"`
			ToolCalls []chatToolCall `json:"tool_calls,omitempty"`
		} `json:"delta"`
	} `json:"choices"`
}

// chatToolCall mirrors the OpenAI streaming tool_call shape — function-call
// style (Chat Completions API) and Responses tool style (when the upstream
// fans out tool steps over the same stream). We only care about the name
// for the badge, not the arguments.
type chatToolCall struct {
	Type     string `json:"type,omitempty"`
	Function struct {
		Name string `json:"name,omitempty"`
	} `json:"function,omitempty"`
}

// StreamEvent is one normalized chunk surfaced to the handler. We split the
// upstream stream into two flavors so SSE can carry text deltas and tool
// signals on different event types without the client having to parse
// provider-specific shapes.
type StreamEvent struct {
	Kind StreamEventKind
	Text string // populated when Kind == StreamEventText
	Tool string // populated when Kind == StreamEventTool — the tool name
}

type StreamEventKind int

const (
	StreamEventText StreamEventKind = iota
	StreamEventTool
)

func (s *ChatService) Complete(ctx context.Context, messages []ChatMessage, opts ChatOptions) (string, error) {
	if !s.Configured() {
		return "", ErrChatNotConfigured
	}
	cleaned, err := normalizeChatMessages(messages)
	if err != nil {
		return "", err
	}
	reqBody := s.buildRequest(withDefaultSystem(cleaned), opts, false)
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(reqBody); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/chat/completions", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", upstreamError(resp)
	}

	var out chatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if len(out.Choices) == 0 {
		return "", errors.New("empty chat completion response")
	}
	return out.Choices[0].Message.Content, nil
}

func (s *ChatService) Stream(ctx context.Context, messages []ChatMessage, opts ChatOptions, emit func(StreamEvent) error) error {
	if !s.Configured() {
		return ErrChatNotConfigured
	}
	cleaned, err := normalizeChatMessages(messages)
	if err != nil {
		return err
	}
	reqBody := s.buildRequest(withDefaultSystem(cleaned), opts, true)
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(reqBody); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/chat/completions", &body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return upstreamError(resp)
	}

	scanner := bufio.NewScanner(resp.Body)
	// Allow large lines — vision chunks can include long base64 echoes in error
	// frames, and some providers stream big lines occasionally.
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	// De-dupe tool emits: each tool name is signaled at most once per response.
	// Some providers stream the tool arguments incrementally and we don't want
	// the badge to "re-arm" on every fragment.
	seenTools := map[string]struct{}{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			return nil
		}
		var chunk chatCompletionChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				if err := emit(StreamEvent{Kind: StreamEventText, Text: choice.Delta.Content}); err != nil {
					return err
				}
			}
			for _, tc := range choice.Delta.ToolCalls {
				name := strings.TrimSpace(tc.Function.Name)
				if name == "" {
					name = strings.TrimSpace(tc.Type)
				}
				if name == "" {
					continue
				}
				if _, ok := seenTools[name]; ok {
					continue
				}
				seenTools[name] = struct{}{}
				if err := emit(StreamEvent{Kind: StreamEventTool, Tool: name}); err != nil {
					return err
				}
			}
		}
	}
	return scanner.Err()
}

// normalizeChatMessages validates the role and content shape of every message,
// then returns the same slice with leading/trailing whitespace trimmed on text.
// Content is left as RawMessage so it forwards verbatim to the upstream API.
func normalizeChatMessages(messages []ChatMessage) ([]ChatMessage, error) {
	if len(messages) == 0 || len(messages) > maxMessages {
		return nil, ErrChatBadRequest
	}
	out := make([]ChatMessage, 0, len(messages))
	for _, m := range messages {
		role := strings.TrimSpace(m.Role)
		switch role {
		case "system", "user", "assistant":
		default:
			return nil, ErrChatBadRequest
		}
		raw := bytes.TrimSpace(m.Content)
		if len(raw) == 0 {
			return nil, ErrChatBadRequest
		}

		// Case 1 — content is a JSON string. Trim, length-check, re-marshal.
		var asString string
		if err := json.Unmarshal(raw, &asString); err == nil {
			asString = strings.TrimSpace(asString)
			if asString == "" || len(asString) > maxTextPartLen {
				return nil, ErrChatBadRequest
			}
			b, err := json.Marshal(asString)
			if err != nil {
				return nil, err
			}
			out = append(out, ChatMessage{Role: role, Content: b})
			continue
		}

		// Case 2 — content is an array of multimodal parts. Validate each.
		var parts []contentPart
		if err := json.Unmarshal(raw, &parts); err != nil {
			return nil, ErrChatBadRequest
		}
		if len(parts) == 0 || len(parts) > maxPartsPerMessage {
			return nil, ErrChatBadRequest
		}
		hasText, hasImage := false, false
		for _, p := range parts {
			switch p.Type {
			case "text":
				t := strings.TrimSpace(p.Text)
				if t == "" || len(t) > maxTextPartLen {
					return nil, ErrChatBadRequest
				}
				hasText = true
			case "image_url":
				// Only user messages may carry images. assistant/system text-only.
				if role != "user" {
					return nil, ErrChatBadRequest
				}
				if p.ImageURL == nil || p.ImageURL.URL == "" {
					return nil, ErrChatBadRequest
				}
				if len(p.ImageURL.URL) > maxImageURLLen {
					return nil, ErrChatBadRequest
				}
				// Only data:image/* and https:// allowed — block file://, javascript:, etc.
				u := p.ImageURL.URL
				if !strings.HasPrefix(u, "data:image/") && !strings.HasPrefix(u, "https://") {
					return nil, ErrChatBadRequest
				}
				hasImage = true
			default:
				return nil, ErrChatBadRequest
			}
		}
		if !hasText && !hasImage {
			return nil, ErrChatBadRequest
		}
		// Pass-through the original raw bytes — already validated.
		out = append(out, ChatMessage{Role: role, Content: raw})
	}
	return out, nil
}

// withDefaultSystem prepends a system prompt if the caller didn't supply one.
func withDefaultSystem(messages []ChatMessage) []ChatMessage {
	for _, m := range messages {
		if m.Role == "system" {
			return messages
		}
	}
	systemContent, _ := json.Marshal(defaultChatSystemPrompt)
	out := make([]ChatMessage, 0, len(messages)+1)
	out = append(out, ChatMessage{Role: "system", Content: systemContent})
	out = append(out, messages...)
	return out
}

func upstreamError(resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	msg := strings.TrimSpace(string(b))
	if msg == "" {
		msg = resp.Status
	}
	return &ChatUpstreamError{StatusCode: resp.StatusCode, Body: msg}
}
