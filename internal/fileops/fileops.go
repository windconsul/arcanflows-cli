package fileops

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/arcanflows/cli/internal/render"
)

// ProtectedPaths are paths that should never be modified.
var ProtectedPaths = []string{
	".git",
	".env",
	"*.key",
	"*.pem",
	"*.p12",
	"*.pfx",
	"id_rsa",
	"id_ed25519",
}

// SetProtectedPaths adds custom protected paths (from ARCANFLOWS.md ## Do Not Touch).
func SetProtectedPaths(paths []string) {
	ProtectedPaths = append(ProtectedPaths, paths...)
}

// IsProtected checks if a path matches any protected pattern.
func IsProtected(path string) bool {
	name := filepath.Base(path)
	for _, pattern := range ProtectedPaths {
		if matched, _ := filepath.Match(pattern, name); matched {
			return true
		}
		if matched, _ := filepath.Match(pattern, path); matched {
			return true
		}
		if name == pattern || path == pattern {
			return true
		}
	}
	return false
}

// CreateFile creates a new file with content, with backup support.
func CreateFile(path, content string, autoConfirm bool) error {
	if IsProtected(path) {
		return fmt.Errorf("path '%s' is protected and cannot be modified", path)
	}

	// Check if file exists
	if _, err := os.Stat(path); err == nil {
		render.Warning(fmt.Sprintf("File already exists: %s", path))
		if !autoConfirm && !confirm("Overwrite?") {
			render.Info("Skipped")
			return nil
		}
		// Backup existing
		backup(path)
	}

	// Create parent directories
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("creating directory %s: %w", dir, err)
		}
	}

	// Confirm
	lines := strings.Count(content, "\n") + 1
	if !autoConfirm {
		fmt.Fprintf(os.Stderr, "\n%s--- Create file: %s (%d lines) ---%s\n", render.Cyan, path, lines, render.Reset)
		showPreview(content, 20)
		if !confirm("Create this file?") {
			render.Info("Skipped")
			return nil
		}
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}

	render.Success(fmt.Sprintf("Created %s (%d lines)", path, lines))
	return nil
}

// EditFile applies content changes to an existing file.
func EditFile(path, newContent string, autoConfirm bool) error {
	if IsProtected(path) {
		return fmt.Errorf("path '%s' is protected and cannot be modified", path)
	}

	// Read current content
	existing, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading file: %w", err)
	}

	if string(existing) == newContent {
		render.Info(fmt.Sprintf("No changes to %s", path))
		return nil
	}

	if !autoConfirm {
		fmt.Fprintf(os.Stderr, "\n%s--- Edit: %s ---%s\n", render.Yellow, path, render.Reset)
		showDiff(string(existing), newContent)
		if !confirm("Apply changes?") {
			render.Info("Skipped")
			return nil
		}
	}

	// Backup
	backup(path)

	if err := os.WriteFile(path, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}

	oldLines := strings.Count(string(existing), "\n")
	newLines := strings.Count(newContent, "\n")
	diff := newLines - oldLines
	sign := "+"
	if diff < 0 {
		sign = ""
	}
	render.Success(fmt.Sprintf("Updated %s (%s%d lines)", path, sign, diff))
	return nil
}

// ReadFile reads a file and returns its content with metadata.
func ReadFile(path string) (string, error) {
	absPath, _ := filepath.Abs(path)
	content, err := os.ReadFile(absPath)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}

	info, _ := os.Stat(absPath)
	lines := strings.Count(string(content), "\n") + 1

	render.Info(fmt.Sprintf("Read %s (%d lines, %s)", path, lines, formatSize(info.Size())))

	return string(content), nil
}

// DeleteFile removes a file with confirmation.
func DeleteFile(path string, autoConfirm bool) error {
	if IsProtected(path) {
		return fmt.Errorf("path '%s' is protected and cannot be deleted", path)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("file not found: %s", path)
	}

	if !autoConfirm {
		fmt.Fprintf(os.Stderr, "\n%s--- Delete: %s ---%s\n", render.Red, path, render.Reset)
		if !confirm("Delete this file?") {
			render.Info("Skipped")
			return nil
		}
	}

	// Backup before delete
	backup(path)

	if err := os.Remove(path); err != nil {
		return fmt.Errorf("deleting file: %w", err)
	}

	render.Success(fmt.Sprintf("Deleted %s", path))
	return nil
}

// UndoLast restores the most recent backup.
func UndoLast() error {
	backupDir := ".arcanflows/backups"
	entries, err := os.ReadDir(backupDir)
	if err != nil || len(entries) == 0 {
		return fmt.Errorf("no backups found")
	}

	// Find most recent
	latest := entries[len(entries)-1]
	backupPath := filepath.Join(backupDir, latest.Name())

	// Parse original path from backup name (format: timestamp_path)
	name := latest.Name()
	parts := strings.SplitN(name, "_", 2)
	if len(parts) < 2 {
		return fmt.Errorf("invalid backup format: %s", name)
	}
	originalPath := strings.ReplaceAll(parts[1], "__", "/")

	content, err := os.ReadFile(backupPath)
	if err != nil {
		return fmt.Errorf("reading backup: %w", err)
	}

	if err := os.WriteFile(originalPath, content, 0644); err != nil {
		return fmt.Errorf("restoring file: %w", err)
	}

	// Remove the backup
	os.Remove(backupPath)

	render.Success(fmt.Sprintf("Restored %s from backup", originalPath))
	return nil
}

// backup creates a backup of a file before modification.
func backup(path string) {
	content, err := os.ReadFile(path)
	if err != nil {
		return // file doesn't exist, no backup needed
	}

	backupDir := ".arcanflows/backups"
	os.MkdirAll(backupDir, 0755)

	// Replace path separators for flat backup naming
	safeName := strings.ReplaceAll(path, "/", "__")
	timestamp := time.Now().Format("20060102_150405")
	backupPath := filepath.Join(backupDir, fmt.Sprintf("%s_%s", timestamp, safeName))

	os.WriteFile(backupPath, content, 0644)
}

// confirm asks the user for y/n confirmation.
func confirm(prompt string) bool {
	fmt.Fprintf(os.Stderr, "%s%s [y/n]:%s ", render.Bold, prompt, render.Reset)
	var response string
	fmt.Scanln(&response)
	response = strings.ToLower(strings.TrimSpace(response))
	return response == "y" || response == "yes"
}

// showPreview shows the first N lines of content.
func showPreview(content string, maxLines int) {
	lines := strings.Split(content, "\n")
	show := lines
	if len(lines) > maxLines {
		show = lines[:maxLines]
	}
	for i, line := range show {
		fmt.Fprintf(os.Stderr, "  %s%3d%s %s\n", render.Dim, i+1, render.Reset, line)
	}
	if len(lines) > maxLines {
		fmt.Fprintf(os.Stderr, "  %s... (%d more lines)%s\n", render.Dim, len(lines)-maxLines, render.Reset)
	}
}

// showDiff shows a simple before/after comparison.
func showDiff(old, new string) {
	oldLines := strings.Split(old, "\n")
	newLines := strings.Split(new, "\n")

	// Simple line-level diff display
	maxShow := 30
	shown := 0

	// Find changed/added/removed lines
	maxLen := len(oldLines)
	if len(newLines) > maxLen {
		maxLen = len(newLines)
	}

	for i := 0; i < maxLen && shown < maxShow; i++ {
		oldLine := ""
		newLine := ""
		if i < len(oldLines) {
			oldLine = oldLines[i]
		}
		if i < len(newLines) {
			newLine = newLines[i]
		}

		if oldLine != newLine {
			if oldLine != "" && i < len(oldLines) {
				fmt.Fprintf(os.Stderr, "  %s- %s%s\n", render.Red, oldLine, render.Reset)
				shown++
			}
			if newLine != "" && i < len(newLines) {
				fmt.Fprintf(os.Stderr, "  %s+ %s%s\n", render.Green, newLine, render.Reset)
				shown++
			}
		}
	}

	if maxLen > maxShow {
		fmt.Fprintf(os.Stderr, "  %s... (diff truncated)%s\n", render.Dim, render.Reset)
	}
}

func formatSize(b int64) string {
	if b < 1024 {
		return fmt.Sprintf("%d B", b)
	}
	return fmt.Sprintf("%.1f KB", float64(b)/1024)
}
