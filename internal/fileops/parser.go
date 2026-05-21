package fileops

import (
	"strings"
)

// FileOperation represents a file operation extracted from agent response.
type FileOperation struct {
	Type    string // "create", "edit", "delete", "rename"
	Path    string
	Content string
	NewPath string // for rename
}

// ParseOperations extracts file operation directives from agent text.
// Detects patterns like:
//
//	--- Create file: path ---
//	content...
//	--- End file ---
//
//	--- Edit: path ---
//	content...
//	--- End edit ---
//
//	--- Delete file: path ---
func ParseOperations(text string) []FileOperation {
	var ops []FileOperation
	lines := strings.Split(text, "\n")

	i := 0
	for i < len(lines) {
		line := strings.TrimSpace(lines[i])

		// Create file
		if isCreateDirective(line) {
			path := extractPath(line, "create file:")
			if path == "" {
				path = extractPath(line, "create:")
			}
			content, endIdx := extractBlock(lines, i+1, "--- end file ---", "--- end ---")
			if content != "" {
				ops = append(ops, FileOperation{
					Type:    "create",
					Path:    path,
					Content: content,
				})
				i = endIdx + 1
				continue
			}
		}

		// Edit file
		if isEditDirective(line) {
			path := extractPath(line, "edit:")
			if path == "" {
				path = extractPath(line, "edit file:")
			}
			content, endIdx := extractBlock(lines, i+1, "--- end edit ---", "--- end ---")
			if content != "" {
				ops = append(ops, FileOperation{
					Type:    "edit",
					Path:    path,
					Content: content,
				})
				i = endIdx + 1
				continue
			}
		}

		// Delete file
		if isDeleteDirective(line) {
			path := extractPath(line, "delete file:")
			if path == "" {
				path = extractPath(line, "delete:")
			}
			if path != "" {
				ops = append(ops, FileOperation{
					Type: "delete",
					Path: path,
				})
			}
		}

		// Rename
		if isRenameDirective(line) {
			old, new_ := extractRenamePaths(line)
			if old != "" && new_ != "" {
				ops = append(ops, FileOperation{
					Type:    "rename",
					Path:    old,
					NewPath: new_,
				})
			}
		}

		i++
	}

	return ops
}

// StripOperations removes file operation blocks from text, leaving clean prose.
func StripOperations(text string) string {
	lines := strings.Split(text, "\n")
	var clean []string
	inBlock := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if isCreateDirective(trimmed) || isEditDirective(trimmed) {
			inBlock = true
			continue
		}
		if inBlock && isEndDirective(trimmed) {
			inBlock = false
			continue
		}
		if isDeleteDirective(trimmed) || isRenameDirective(trimmed) {
			continue
		}
		if inBlock {
			continue
		}
		clean = append(clean, line)
	}

	return strings.TrimSpace(strings.Join(clean, "\n"))
}

func isCreateDirective(line string) bool {
	lower := strings.ToLower(line)
	return strings.HasPrefix(lower, "--- create file:") || strings.HasPrefix(lower, "--- create:")
}

func isEditDirective(line string) bool {
	lower := strings.ToLower(line)
	return strings.HasPrefix(lower, "--- edit:") || strings.HasPrefix(lower, "--- edit file:")
}

func isDeleteDirective(line string) bool {
	lower := strings.ToLower(line)
	return strings.HasPrefix(lower, "--- delete file:") || strings.HasPrefix(lower, "--- delete:")
}

func isRenameDirective(line string) bool {
	lower := strings.ToLower(line)
	return strings.HasPrefix(lower, "--- rename:")
}

func isEndDirective(line string) bool {
	lower := strings.ToLower(line)
	return lower == "--- end file ---" || lower == "--- end edit ---" || lower == "--- end ---"
}

func extractPath(line, prefix string) string {
	lower := strings.ToLower(line)
	idx := strings.Index(lower, strings.ToLower(prefix))
	if idx < 0 {
		return ""
	}
	rest := line[idx+len(prefix):]
	rest = strings.TrimSpace(rest)
	rest = strings.TrimSuffix(rest, "---")
	rest = strings.TrimSpace(rest)
	// Remove backticks if wrapped
	rest = strings.Trim(rest, "`")
	return rest
}

func extractBlock(lines []string, startIdx int, endMarkers ...string) (string, int) {
	var content []string
	for i := startIdx; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		lower := strings.ToLower(trimmed)
		for _, marker := range endMarkers {
			if lower == marker {
				return strings.Join(content, "\n"), i
			}
		}
		content = append(content, lines[i])
	}
	// No end marker found — use all remaining content
	if len(content) > 0 {
		return strings.Join(content, "\n"), len(lines) - 1
	}
	return "", startIdx
}

func extractRenamePaths(line string) (string, string) {
	// Format: --- Rename: old -> new ---
	lower := strings.ToLower(line)
	idx := strings.Index(lower, "rename:")
	if idx < 0 {
		return "", ""
	}
	rest := line[idx+7:]
	rest = strings.TrimSuffix(rest, "---")
	rest = strings.TrimSpace(rest)

	parts := strings.SplitN(rest, "->", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}
