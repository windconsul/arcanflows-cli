package fileops

import (
	"strings"
)

// ReplaceEdit is one search/replace pair within a "replace" operation.
type ReplaceEdit struct {
	Search  string
	Replace string
}

// FileOperation represents a file operation extracted from agent response.
type FileOperation struct {
	Type         string // "create", "edit", "replace", "delete", "rename"
	Path         string
	Content      string
	NewPath      string        // for rename
	Replacements []ReplaceEdit // for "replace" (anchored/diff edits)
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
// stripMD removes surrounding markdown emphasis/code ticks from a directive
// line so a model that writes `**--- Create file: x ---**` or wraps directives
// in backticks still triggers the operation (PROPOSAL-010 C6 — the same
// bold-directive bug fixed platform-side).
func stripMD(s string) string {
	return strings.TrimSpace(strings.Trim(strings.TrimSpace(s), "*`"))
}

func ParseOperations(text string) []FileOperation {
	var ops []FileOperation
	lines := strings.Split(text, "\n")

	i := 0
	for i < len(lines) {
		line := stripMD(lines[i])

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

		// Replace (anchored search/replace edit) — checked before Edit so
		// "--- Replace: ..." isn't mistaken for anything else.
		if isReplaceDirective(line) {
			path := extractPath(line, "replace in:")
			if path == "" {
				path = extractPath(line, "replace:")
			}
			content, endIdx := extractBlock(lines, i+1, "--- end replace ---", "--- end ---")
			edits := parseReplaceBlocks(strings.Split(content, "\n"))
			if path != "" && len(edits) > 0 {
				ops = append(ops, FileOperation{
					Type:         "replace",
					Path:         path,
					Replacements: edits,
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
		trimmed := stripMD(line)

		if isCreateDirective(trimmed) || isEditDirective(trimmed) || isReplaceDirective(trimmed) {
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

func isReplaceDirective(line string) bool {
	lower := strings.ToLower(line)
	return strings.HasPrefix(lower, "--- replace:") || strings.HasPrefix(lower, "--- replace in:")
}

// parseReplaceBlocks parses conflict-marker search/replace blocks:
//
//	<<<<<<< SEARCH
//	old text
//	=======
//	new text
//	>>>>>>> REPLACE
func parseReplaceBlocks(lines []string) []ReplaceEdit {
	var edits []ReplaceEdit
	var search, replace []string
	state := 0 // 0=outside, 1=in search, 2=in replace
	for _, line := range lines {
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, "<<<<<<<"):
			state = 1
			search, replace = nil, nil
		case state != 0 && strings.HasPrefix(t, "======="):
			state = 2
		case state != 0 && strings.HasPrefix(t, ">>>>>>>"):
			edits = append(edits, ReplaceEdit{
				Search:  strings.Join(search, "\n"),
				Replace: strings.Join(replace, "\n"),
			})
			state = 0
		default:
			if state == 1 {
				search = append(search, line)
			} else if state == 2 {
				replace = append(replace, line)
			}
		}
	}
	return edits
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
	return lower == "--- end file ---" || lower == "--- end edit ---" ||
		lower == "--- end replace ---" || lower == "--- end ---"
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
		lower := strings.ToLower(stripMD(lines[i]))
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
