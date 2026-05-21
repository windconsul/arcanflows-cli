package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Entry represents a single memory item.
type Entry struct {
	ID        int       `yaml:"id"`
	Content   string    `yaml:"content"`
	Source    string    `yaml:"source,omitempty"` // "user", "auto"
	CreatedAt time.Time `yaml:"created_at"`
}

// Store holds persistent memory entries.
type Store struct {
	Entries []Entry `yaml:"entries"`
	path    string
}

// Dir returns the memory storage directory.
func Dir() string {
	return ".arcanflows/memory"
}

// Load reads the memory store from disk.
func Load() (*Store, error) {
	dir := Dir()
	path := filepath.Join(dir, "project.yaml")

	store := &Store{path: path}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return store, nil
		}
		return nil, err
	}

	if err := yaml.Unmarshal(data, store); err != nil {
		return nil, err
	}
	store.path = path
	return store, nil
}

// Save writes the memory store to disk.
func (s *Store) Save() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := yaml.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0644)
}

// Add creates a new memory entry.
func (s *Store) Add(content, source string) int {
	maxID := 0
	for _, e := range s.Entries {
		if e.ID > maxID {
			maxID = e.ID
		}
	}
	entry := Entry{
		ID:        maxID + 1,
		Content:   content,
		Source:    source,
		CreatedAt: time.Now(),
	}
	s.Entries = append(s.Entries, entry)
	return entry.ID
}

// Remove deletes a memory entry by ID.
func (s *Store) Remove(id int) bool {
	for i, e := range s.Entries {
		if e.ID == id {
			s.Entries = append(s.Entries[:i], s.Entries[i+1:]...)
			return true
		}
	}
	return false
}

// Clear removes all entries.
func (s *Store) Clear() {
	s.Entries = nil
}

// BuildContext formats all memory entries as context for the agent prompt.
func (s *Store) BuildContext() string {
	if len(s.Entries) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Project Memory (persistent context)\n\n")
	for _, e := range s.Entries {
		sb.WriteString(fmt.Sprintf("- %s\n", e.Content))
	}
	sb.WriteString("\n---\n\n")
	return sb.String()
}

// Count returns the number of entries.
func (s *Store) Count() int {
	return len(s.Entries)
}
