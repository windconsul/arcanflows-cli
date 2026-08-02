// Package ledger records local development/agent usage (PROPOSAL-010 §6.3,
// D2: on by default — the file lives on the user's disk and nowhere else).
// One JSON line per agent turn: what it cost, what it touched, what it ran.
// The API has always returned token counts; before this the CLI printed and
// discarded them.
package ledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type Entry struct {
	Time             time.Time `json:"time"`
	Agent            string    `json:"agent"`
	ConversationID   string    `json:"conversation_id,omitempty"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	FilesTouched     []string  `json:"files_touched,omitempty"`
	CommandsRun      []string  `json:"commands_run,omitempty"`
	DurationSeconds  float64   `json:"duration_s"`
}

func path() string { return filepath.Join(".arcanflows", "usage.jsonl") }

// Append is best-effort: usage tracking must never break a chat turn.
func Append(e Entry) {
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	if err := os.MkdirAll(".arcanflows", 0755); err != nil {
		return
	}
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	f, err := os.OpenFile(path(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(data, '\n'))
}

// ReadSince returns entries newer than the cutoff (zero = all).
func ReadSince(cutoff time.Time) ([]Entry, error) {
	data, err := os.ReadFile(path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Entry
	for _, line := range splitLines(string(data)) {
		var e Entry
		if json.Unmarshal([]byte(line), &e) != nil {
			continue
		}
		if cutoff.IsZero() || e.Time.After(cutoff) {
			out = append(out, e)
		}
	}
	return out, nil
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			if i > start {
				out = append(out, s[start:i])
			}
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}
