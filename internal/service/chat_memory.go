package service

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/lilce/blog-api/internal/model"
	"github.com/lilce/blog-api/internal/repository"
)

const (
	// memoryExtractEvery — how many user-turns between extraction passes.
	// Every 4 turns is the plan-confirmed cadence: small enough to feel
	// responsive (memories arrive while you're still in the conversation),
	// big enough that we add ~25% LLM call volume rather than 100%.
	memoryExtractEvery = 4

	// memoryRecentWindow — how many trailing messages are fed to the
	// extractor. Bigger than memoryExtractEvery on purpose: we want the
	// extractor to see preceding assistant turns for context, but bounded
	// so the prompt stays cheap.
	memoryRecentWindow = 12

	// memoryInjectN — top-N most recent memories prepended to the system
	// prompt on every admin chat call. 20 is a soft ceiling — the model's
	// context budget can absorb 20 × ~120-char facts (~3 KB) without
	// noticeable bloat.
	memoryInjectN = 20

	// maxMemoriesPerUser — ring-buffer cap. After every Extract, oldest
	// rows past this count get evicted.
	maxMemoriesPerUser = 200

	memoryMaxLen = 200 // truncate any extracted fact to this length
)

const memoryExtractSystem = `You are a memory-extraction assistant. Read the conversation
that follows and identify any durable, useful facts the user shared about themselves —
their preferences, projects, expertise, constraints, goals, or context that would help
a future assistant respond more relevantly.

Rules:
- Output ONLY a JSON array of short strings. No commentary, no markdown, no code fences.
- Each string is a third-person factual statement, ≤120 characters, e.g. "User is a frontend engineer".
- If nothing notable was shared, output an empty array: [].
- Skip transient state, ephemeral questions, or things the user is exploring without commitment.
- Do not extract anything that's already obviously general knowledge.`

// MemoryService extracts and injects long-term user memories.
//
// Extraction is synchronous on the request thread — every Nth user turn we
// fire one extra Complete call to the same upstream. This adds a 1–3s tail
// on those turns; the alternative (background goroutine) was rejected in the
// plan because it adds panic / shutdown / observability surface for the
// modest gain.
type MemoryService struct {
	repo *repository.ChatMemoryRepository
	chat *ChatService
}

func NewMemoryService(repo *repository.ChatMemoryRepository, chat *ChatService) *MemoryService {
	return &MemoryService{repo: repo, chat: chat}
}

// ShouldExtract returns true when the user-message count for this session
// has crossed an extraction boundary. Caller passes in the count from the
// chat_messages repo so this stays a pure check.
func ShouldExtract(userMsgCount int) bool {
	return userMsgCount > 0 && userMsgCount%memoryExtractEvery == 0
}

// MemoryExtractWindow returns the message-window size the handler should
// pull from history before calling Extract.
func MemoryExtractWindow() int { return memoryRecentWindow }

// Extract runs one memory-extraction pass on the supplied recent messages
// and persists newly-discovered facts under userID. Returns the strings
// stored (after dedupe + eviction) so the caller can log / surface them.
//
// On any non-fatal extraction failure (upstream blip, malformed JSON
// response, empty array) we return nil-slice + nil-error: extraction is a
// best-effort enrichment and must not break the user's chat turn.
func (s *MemoryService) Extract(ctx context.Context, userID uint64, recent []ChatMessage, sourceSessionID *uint64) ([]string, error) {
	if s == nil || s.repo == nil || s.chat == nil {
		return nil, nil
	}
	if !s.chat.Configured() {
		return nil, ErrChatNotConfigured
	}
	if len(recent) == 0 {
		return nil, nil
	}

	// Build the extraction request. We send: a system prompt that defines
	// the task, a single user prompt that contains a serialization of the
	// last few turns. We avoid streaming — the body is small.
	transcript, err := flattenForExtraction(recent)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(transcript) == "" {
		return nil, nil
	}

	sysB, _ := json.Marshal(memoryExtractSystem)
	userB, _ := json.Marshal("Conversation transcript:\n\n" + transcript)
	msgs := []ChatMessage{
		{Role: "system", Content: sysB},
		{Role: "user", Content: userB},
	}

	// Use a short timeout independent of the parent — extraction shouldn't
	// hang the chat turn for a minute.
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Force a low-temperature, default model — we don't want extraction to
	// burn the user's premium model selection.
	resp, err := s.chat.Complete(cctx, msgs, ChatOptions{DeepThinking: false})
	if err != nil {
		// Soft-fail: log + return nil so the user's chat turn isn't impacted.
		log.Printf("memory: extraction call failed: %v", err)
		return nil, nil
	}
	facts := parseExtractionFacts(resp)
	if len(facts) == 0 {
		return nil, nil
	}

	stored := make([]string, 0, len(facts))
	for _, fact := range facts {
		fact = trimFact(fact)
		if fact == "" {
			continue
		}
		if exists, err := s.repo.ExistsContent(userID, fact); err == nil && exists {
			continue
		}
		mem := &model.ChatMemory{
			UserID:          userID,
			Content:         fact,
			SourceSessionID: sourceSessionID,
		}
		if err := s.repo.Create(mem); err != nil {
			log.Printf("memory: persist failed: %v", err)
			continue
		}
		stored = append(stored, fact)
	}

	if len(stored) > 0 {
		if err := s.repo.EvictOldest(userID, maxMemoriesPerUser); err != nil {
			log.Printf("memory: evict failed: %v", err)
		}
	}
	return stored, nil
}

// Inject prepends the user's latest memories into the system prompt of an
// outgoing message slice. If the slice has no system message, one is
// inserted; if it has one, the memory dump is appended to its content.
//
// Returns the (possibly new) slice; the caller should use this rather than
// the original. We never mutate input messages.
func (s *MemoryService) Inject(ctx context.Context, userID uint64, msgs []ChatMessage) []ChatMessage {
	if s == nil || s.repo == nil || userID == 0 {
		return msgs
	}
	mems, err := s.repo.LatestNByUser(userID, memoryInjectN)
	if err != nil || len(mems) == 0 {
		if err != nil {
			log.Printf("memory: read for inject failed: %v", err)
		}
		return msgs
	}

	dump := buildMemoryDump(mems)
	if dump == "" {
		return msgs
	}

	// Find an existing system message; if present, append. Otherwise
	// prepend a new system message holding only the dump.
	out := make([]ChatMessage, 0, len(msgs)+1)
	injected := false
	for _, m := range msgs {
		if !injected && m.Role == "system" {
			// Decode existing string content (the only shape system messages
			// take in this codebase) and append the dump.
			var existing string
			if err := json.Unmarshal(m.Content, &existing); err != nil {
				// Defensive: if it's not a string, leave it alone and emit
				// a separate system message after.
				out = append(out, m)
				appendB, _ := json.Marshal(dump)
				out = append(out, ChatMessage{Role: "system", Content: appendB})
				injected = true
				continue
			}
			merged := strings.TrimRight(existing, "\n") + "\n\n" + dump
			b, _ := json.Marshal(merged)
			out = append(out, ChatMessage{Role: "system", Content: b})
			injected = true
			continue
		}
		out = append(out, m)
	}
	if !injected {
		b, _ := json.Marshal(dump)
		out = append([]ChatMessage{{Role: "system", Content: b}}, out...)
	}
	return out
}

// flattenForExtraction renders a few recent turns as a plaintext transcript
// the extractor can read. We unmarshal each message's content to a string
// where possible, dropping multimodal parts (images don't carry persistent
// facts about the user that aren't already in the text).
func flattenForExtraction(msgs []ChatMessage) (string, error) {
	var b strings.Builder
	for _, m := range msgs {
		role := m.Role
		// Try plain string first.
		var s string
		if err := json.Unmarshal(m.Content, &s); err == nil {
			if strings.TrimSpace(s) == "" {
				continue
			}
			b.WriteString(strings.ToUpper(role[:1]) + role[1:] + ":\n")
			b.WriteString(strings.TrimSpace(s))
			b.WriteString("\n\n")
			continue
		}
		// Multimodal — extract text parts only.
		var parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(m.Content, &parts); err != nil {
			continue
		}
		text := ""
		for _, p := range parts {
			if p.Type == "text" {
				text += p.Text + " "
			}
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		b.WriteString(strings.ToUpper(role[:1]) + role[1:] + ":\n")
		b.WriteString(text)
		b.WriteString("\n\n")
	}
	return b.String(), nil
}

// parseExtractionFacts extracts a JSON string array from the model's reply.
// Models routinely wrap valid JSON in code fences or surround it with
// chatter; we strip those before parsing. Returns nil on any failure —
// callers treat extraction as best-effort.
func parseExtractionFacts(reply string) []string {
	s := strings.TrimSpace(reply)
	// Strip leading/trailing code fences if present.
	if strings.HasPrefix(s, "```") {
		// Drop the first line up to a newline.
		if nl := strings.Index(s, "\n"); nl >= 0 {
			s = s[nl+1:]
		}
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	}
	// Sometimes the model wraps an array in narrative text. Locate the
	// first `[` and the matching last `]`.
	openIdx := strings.Index(s, "[")
	closeIdx := strings.LastIndex(s, "]")
	if openIdx < 0 || closeIdx < openIdx {
		return nil
	}
	s = s[openIdx : closeIdx+1]

	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		// Single fallback: trailing-comma fix some lax models emit. Re-try
		// once; if that fails too, give up — we don't promise to recover
		// from arbitrary malformed JSON.
		alt := trimTrailingCommas(strings.ReplaceAll(s, "'", "\""))
		if err2 := json.Unmarshal([]byte(alt), &out); err2 != nil {
			return nil
		}
	}
	return out
}

// trimTrailingCommas — removes ", ]" / ", }" patterns that some models emit.
func trimTrailingCommas(s string) string {
	s = strings.ReplaceAll(s, ",]", "]")
	s = strings.ReplaceAll(s, ", ]", "]")
	s = strings.ReplaceAll(s, ",\n]", "\n]")
	return s
}

func trimFact(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// Strip stray quotes, leading bullets, and the "User " prefix duplicates.
	s = strings.Trim(s, "-•*\"' \t")
	s = strings.TrimPrefix(s, "- ")
	if len(s) > memoryMaxLen {
		s = s[:memoryMaxLen-1] + "…"
	}
	return s
}

func buildMemoryDump(mems []model.ChatMemory) string {
	if len(mems) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("[User memories — facts the user has shared previously, draw on them when relevant:]\n")
	for _, m := range mems {
		c := strings.TrimSpace(m.Content)
		if c == "" {
			continue
		}
		b.WriteString("- ")
		b.WriteString(c)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
