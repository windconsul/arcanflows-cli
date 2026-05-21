package session

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Message represents a single chat message in a session.
type Message struct {
	Role      string    `yaml:"role"`      // "user" or "assistant"
	Content   string    `yaml:"content"`   // Message text (truncated for storage)
	Timestamp time.Time `yaml:"timestamp"`
}

// Session represents a saved chat session.
type Session struct {
	ID             string    `yaml:"id"`
	AgentID        string    `yaml:"agent_id"`
	AgentName      string    `yaml:"agent_name"`
	ConversationID string    `yaml:"conversation_id"`
	Summary        string    `yaml:"summary"`         // First user message (truncated)
	StartedAt      time.Time `yaml:"started_at"`
	LastActivityAt time.Time `yaml:"last_activity_at"`
	MessageCount   int       `yaml:"message_count"`
	FilesModified  []string  `yaml:"files_modified,omitempty"`
	CommandsRun    []string  `yaml:"commands_run,omitempty"`
	Messages       []Message `yaml:"messages,omitempty"`
}

// Store manages session persistence.
type Store struct {
	Dir        string
	MaxAgeDays int
}

// DefaultDir returns the default sessions directory.
func DefaultDir() string {
	if d := os.Getenv("ARCANFLOWS_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "history", "sessions")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".arcanflows", "history", "sessions")
}

// NewStore creates a session store with the default directory.
func NewStore(maxAgeDays int) *Store {
	if maxAgeDays <= 0 {
		maxAgeDays = 30
	}
	return &Store{
		Dir:        DefaultDir(),
		MaxAgeDays: maxAgeDays,
	}
}

// sessionFilePath returns the file path for a session.
func (s *Store) sessionFilePath(id string) string {
	return filepath.Join(s.Dir, id+".yaml")
}

// Save persists a session to disk.
func (s *Store) Save(session *Session) error {
	if err := os.MkdirAll(s.Dir, 0700); err != nil {
		return fmt.Errorf("creating sessions dir: %w", err)
	}

	data, err := yaml.Marshal(session)
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}

	return os.WriteFile(s.sessionFilePath(session.ID), data, 0600)
}

// Load reads a session from disk by ID.
func (s *Store) Load(id string) (*Session, error) {
	data, err := os.ReadFile(s.sessionFilePath(id))
	if err != nil {
		return nil, fmt.Errorf("session '%s' not found", id)
	}

	var session Session
	if err := yaml.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("parsing session: %w", err)
	}
	return &session, nil
}

// List returns all sessions sorted by last activity (newest first).
func (s *Store) List() ([]*Session, error) {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var sessions []*Session
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}

		id := strings.TrimSuffix(entry.Name(), ".yaml")
		session, err := s.Load(id)
		if err != nil {
			continue // skip corrupt files
		}
		sessions = append(sessions, session)
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].LastActivityAt.After(sessions[j].LastActivityAt)
	})

	return sessions, nil
}

// Latest returns the most recent session, or nil if none.
func (s *Store) Latest() (*Session, error) {
	sessions, err := s.List()
	if err != nil {
		return nil, err
	}
	if len(sessions) == 0 {
		return nil, nil
	}
	return sessions[0], nil
}

// Delete removes a session by ID.
func (s *Store) Delete(id string) error {
	path := s.sessionFilePath(id)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("session '%s' not found", id)
	}
	return os.Remove(path)
}

// Clear removes all sessions.
func (s *Store) Clear() (int, error) {
	sessions, err := s.List()
	if err != nil {
		return 0, err
	}

	count := 0
	for _, session := range sessions {
		if err := os.Remove(s.sessionFilePath(session.ID)); err == nil {
			count++
		}
	}
	return count, nil
}

// Cleanup removes sessions older than MaxAgeDays.
func (s *Store) Cleanup() (int, error) {
	sessions, err := s.List()
	if err != nil {
		return 0, err
	}

	cutoff := time.Now().AddDate(0, 0, -s.MaxAgeDays)
	count := 0
	for _, session := range sessions {
		if session.LastActivityAt.Before(cutoff) {
			if err := os.Remove(s.sessionFilePath(session.ID)); err == nil {
				count++
			}
		}
	}
	return count, nil
}

// GenerateID creates a session ID from timestamp.
func GenerateID() string {
	return time.Now().Format("20060102-150405")
}

// NewSession creates a new session.
func NewSession(agentID, agentName string) *Session {
	now := time.Now()
	return &Session{
		ID:             GenerateID(),
		AgentID:        agentID,
		AgentName:      agentName,
		StartedAt:      now,
		LastActivityAt: now,
	}
}

// AddMessage appends a message to the session (truncated for storage).
func (sess *Session) AddMessage(role, content string) {
	// Truncate content for storage (keep full content up to 500 chars)
	stored := content
	if len(stored) > 500 {
		stored = stored[:500] + "..."
	}

	sess.Messages = append(sess.Messages, Message{
		Role:      role,
		Content:   stored,
		Timestamp: time.Now(),
	})

	sess.MessageCount = len(sess.Messages)
	sess.LastActivityAt = time.Now()

	// Set summary from first user message
	if sess.Summary == "" && role == "user" {
		summary := content
		if len(summary) > 80 {
			summary = summary[:80] + "..."
		}
		// Single line
		if idx := strings.IndexByte(summary, '\n'); idx > 0 {
			summary = summary[:idx]
		}
		sess.Summary = summary
	}
}

// AddFileModified tracks a file modification.
func (sess *Session) AddFileModified(path string) {
	for _, f := range sess.FilesModified {
		if f == path {
			return // already tracked
		}
	}
	sess.FilesModified = append(sess.FilesModified, path)
	sess.LastActivityAt = time.Now()
}

// AddCommandRun tracks a command execution.
func (sess *Session) AddCommandRun(command string) {
	// Truncate long commands
	if len(command) > 200 {
		command = command[:200] + "..."
	}
	sess.CommandsRun = append(sess.CommandsRun, command)
	sess.LastActivityAt = time.Now()
}

// TimeAgo returns a human-readable time since last activity.
func TimeAgo(t time.Time) string {
	d := time.Since(t)

	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		mins := int(d.Minutes())
		if mins == 1 {
			return "1 min ago"
		}
		return fmt.Sprintf("%d mins ago", mins)
	case d < 24*time.Hour:
		hours := int(d.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	case d < 7*24*time.Hour:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	default:
		weeks := int(d.Hours() / 24 / 7)
		if weeks == 1 {
			return "1 week ago"
		}
		return fmt.Sprintf("%d weeks ago", weeks)
	}
}
