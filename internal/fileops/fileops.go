package fileops

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/arcanflows/cli/internal/render"
)

// backupDir holds content snapshots + the operation journal used by /undo.
const backupDir = ".arcanflows/backups"

// maxBackupFiles caps retained backup snapshots (PROPOSAL-010 C23).
const maxBackupFiles = 100

// ProtectedPaths are paths that should never be modified. The `.env*` glob
// covers .env, .env.local, .env.production, etc. (PROPOSAL-010 C7); the .git
// tree is protected by segment check in IsProtected.
var ProtectedPaths = []string{
	".git",
	".env*",     // .env, .env.local, .env.production, .envrc, ...
	"*.key",
	"*.pem",
	"*.p8",
	"*.p12",
	"*.pfx",
	"*.keystore",
	"*.jks",
	"credentials",
	"credentials.*",
	"secrets",
	"secrets.*",
	".npmrc",
	".netrc",
	"id_rsa",
	"id_ed25519",
}

// SetProtectedPaths adds custom protected paths (from ARCANFLOWS.md ## Do Not Touch).
func SetProtectedPaths(paths []string) {
	ProtectedPaths = append(ProtectedPaths, paths...)
}

// IsProtected checks if a path matches any protected pattern. It matches on the
// basename, on the full (cleaned) path, and — for .git — on any path segment,
// so an agent can't write into a protected directory (e.g. .git/hooks/...).
func IsProtected(path string) bool {
	clean := filepath.Clean(path)
	for _, seg := range strings.Split(clean, string(filepath.Separator)) {
		if seg == ".git" {
			return true
		}
	}
	name := filepath.Base(clean)
	for _, pattern := range ProtectedPaths {
		if matched, _ := filepath.Match(pattern, name); matched {
			return true
		}
		if matched, _ := filepath.Match(pattern, clean); matched {
			return true
		}
		if name == pattern || clean == pattern {
			return true
		}
	}
	return false
}

// allowOutside, when true, disables project-root confinement (the
// --allow-outside escape hatch, PROPOSAL-010 C2).
var allowOutside bool

// SetAllowOutside toggles project-root confinement off.
func SetAllowOutside(v bool) { allowOutside = v }

// confinedAbs resolves path to an absolute path and refuses anything that
// escapes the current project directory (../, absolute paths, or a symlinked
// ancestor pointing outside), unless allowOutside is set. Returns the safe
// absolute path to operate on.
func confinedAbs(path string) (string, error) {
	root, err := os.Getwd()
	if err != nil {
		return "", err
	}
	root, _ = filepath.Abs(root)

	var abs string
	if filepath.IsAbs(path) {
		abs = filepath.Clean(path)
	} else {
		abs = filepath.Clean(filepath.Join(root, path))
	}

	if allowOutside {
		return abs, nil
	}

	// Lexical check first (catches ../ and absolute escapes).
	if !within(root, abs) {
		return "", fmt.Errorf("refusing to touch %q: outside the project directory (use --allow-outside to override)", path)
	}

	// Symlink check: resolve the nearest existing ancestor and ensure it's still
	// inside the (resolved) root — defeats a symlinked dir that escapes.
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		rootReal = root
	}
	ancestor := abs
	for {
		if _, err := os.Lstat(ancestor); err == nil {
			if real, err := filepath.EvalSymlinks(ancestor); err == nil {
				if !within(rootReal, real) {
					return "", fmt.Errorf("refusing to touch %q: resolves outside the project directory", path)
				}
			}
			break
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			break
		}
		ancestor = parent
	}

	return abs, nil
}

// within reports whether target is root itself or nested under it.
func within(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// CreateFile creates a new file with content, with backup support.
func CreateFile(path, content string, autoConfirm bool) error {
	if IsProtected(path) {
		return fmt.Errorf("path '%s' is protected and cannot be modified", path)
	}
	if _, err := confinedAbs(path); err != nil {
		return err
	}

	// Check if file exists
	existed := false
	backupPath := ""
	if _, err := os.Stat(path); err == nil {
		existed = true
		render.Warning(fmt.Sprintf("File already exists: %s", path))
		if !autoConfirm && !confirm("Overwrite?") {
			render.Info("Skipped")
			return nil
		}
		backupPath = backup(path)
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

	if err := writeFileAtomic(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}

	// Record for /undo: overwriting an existing file restores it; a genuinely
	// new file is removed (PROPOSAL-010 C16).
	if existed {
		recordOp("edit", path, "", backupPath)
	} else {
		recordOp("create", path, "", "")
	}

	render.Success(fmt.Sprintf("Created %s (%d lines)", path, lines))
	return nil
}

// EditFile applies content changes to an existing file.
func EditFile(path, newContent string, autoConfirm bool) error {
	if IsProtected(path) {
		return fmt.Errorf("path '%s' is protected and cannot be modified", path)
	}
	if _, err := confinedAbs(path); err != nil {
		return err
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

	backupPath := backup(path)
	mode := fileMode(path)
	if err := writeFileAtomic(path, []byte(newContent), mode); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}
	recordOp("edit", path, "", backupPath)

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

// ReplaceInFile applies one or more anchored search/replace edits to a file
// (PROPOSAL-010 3.1). Each SEARCH must match exactly once — 0 matches or an
// ambiguous match returns a clear error the agent loop can act on, instead of
// silently editing the wrong place or forcing a whole-file rewrite.
func ReplaceInFile(path string, edits []ReplaceEdit, autoConfirm bool) error {
	if IsProtected(path) {
		return fmt.Errorf("path '%s' is protected and cannot be modified", path)
	}
	if _, err := confinedAbs(path); err != nil {
		return err
	}
	existing, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading file: %w", err)
	}
	content := string(existing)
	for i, e := range edits {
		if e.Search == "" {
			return fmt.Errorf("edit %d: empty SEARCH block", i+1)
		}
		switch strings.Count(content, e.Search) {
		case 0:
			return fmt.Errorf("edit %d: SEARCH text not found in %s", i+1, path)
		case 1:
			content = strings.Replace(content, e.Search, e.Replace, 1)
		default:
			return fmt.Errorf("edit %d: SEARCH text matches multiple places in %s — include more surrounding context to make it unique", i+1, path)
		}
	}
	if content == string(existing) {
		render.Info(fmt.Sprintf("No changes to %s", path))
		return nil
	}

	if !autoConfirm {
		fmt.Fprintf(os.Stderr, "\n%s--- Replace in: %s (%d edit(s)) ---%s\n", render.Yellow, path, len(edits), render.Reset)
		showDiff(string(existing), content)
		if !confirm("Apply changes?") {
			render.Info("Skipped")
			return nil
		}
	}

	backupPath := backup(path)
	mode := fileMode(path)
	if err := writeFileAtomic(path, []byte(content), mode); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}
	recordOp("replace", path, "", backupPath)
	render.Success(fmt.Sprintf("Updated %s (%d edit(s))", path, len(edits)))
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
	if _, err := confinedAbs(path); err != nil {
		return err
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
	backupPath := backup(path)

	if err := os.Remove(path); err != nil {
		return fmt.Errorf("deleting file: %w", err)
	}
	recordOp("delete", path, "", backupPath)

	render.Success(fmt.Sprintf("Deleted %s", path))
	return nil
}

// MoveFile renames/moves a file through the SAME guards as the other ops
// (PROPOSAL-010 C3): both endpoints must be inside the project (unless
// --allow-outside), neither may be a protected path, a backup is taken, and the
// move is confirmed. Previously chat.go called os.Rename directly, bypassing
// every safety control.
func MoveFile(oldPath, newPath string, autoConfirm bool) error {
	if IsProtected(oldPath) || IsProtected(newPath) {
		return fmt.Errorf("refusing to move: '%s' or '%s' is a protected path", oldPath, newPath)
	}
	if _, err := confinedAbs(oldPath); err != nil {
		return err
	}
	if _, err := confinedAbs(newPath); err != nil {
		return err
	}
	if _, err := os.Stat(oldPath); os.IsNotExist(err) {
		return fmt.Errorf("file not found: %s", oldPath)
	}
	if _, err := os.Stat(newPath); err == nil {
		render.Warning(fmt.Sprintf("Destination already exists: %s", newPath))
		if !autoConfirm && !confirm("Overwrite destination?") {
			render.Info("Skipped")
			return nil
		}
	}

	if !autoConfirm {
		fmt.Fprintf(os.Stderr, "\n%s--- Move: %s -> %s ---%s\n", render.Yellow, oldPath, newPath, render.Reset)
		if !confirm("Move this file?") {
			render.Info("Skipped")
			return nil
		}
	}

	backupPath := backup(oldPath)

	if dir := filepath.Dir(newPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("creating directory %s: %w", dir, err)
		}
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		return fmt.Errorf("moving file: %w", err)
	}
	recordOp("move", newPath, oldPath, backupPath)
	render.Success(fmt.Sprintf("Moved %s -> %s", oldPath, newPath))
	return nil
}

// journalEntry records one reversible operation for /undo.
type journalEntry struct {
	Type     string `json:"type"`               // create, edit, replace, delete, move
	Path     string `json:"path"`               // resulting file (new path for move/create)
	OrigPath string `json:"orig_path,omitempty"` // move: the original path
	Backup   string `json:"backup,omitempty"`    // snapshot of prior content (empty for create)
	Time     string `json:"time"`
}

func journalPath() string { return filepath.Join(backupDir, "journal.jsonl") }

// recordOp appends a reversible operation to the journal (PROPOSAL-010 C16).
func recordOp(opType, path, origPath, backup string) {
	_ = os.MkdirAll(backupDir, 0755)
	entry := journalEntry{Type: opType, Path: path, OrigPath: origPath, Backup: backup, Time: time.Now().Format(time.RFC3339Nano)}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	f, err := os.OpenFile(journalPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(data, '\n'))
}

// UndoLast reverses the most recent operation using the journal — precise and
// reliable, unlike the old lexically-last-backup guess (PROPOSAL-010 C16). It
// can undo a create (removes the file) and never mangles paths.
func UndoLast() error {
	data, err := os.ReadFile(journalPath())
	if err != nil || len(strings.TrimSpace(string(data))) == 0 {
		return fmt.Errorf("nothing to undo")
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	idx := len(lines) - 1
	for ; idx >= 0 && strings.TrimSpace(lines[idx]) == ""; idx-- {
	}
	if idx < 0 {
		return fmt.Errorf("nothing to undo")
	}
	var last journalEntry
	if err := json.Unmarshal([]byte(lines[idx]), &last); err != nil {
		return fmt.Errorf("corrupt undo journal")
	}

	switch last.Type {
	case "create":
		if err := os.Remove(last.Path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("undo create: %w", err)
		}
		render.Success(fmt.Sprintf("Removed %s (undo create)", last.Path))
	case "move":
		if err := os.Rename(last.Path, last.OrigPath); err != nil {
			return fmt.Errorf("undo move: %w", err)
		}
		render.Success(fmt.Sprintf("Moved back %s -> %s", last.Path, last.OrigPath))
	default: // edit, replace, delete
		if last.Backup == "" {
			return fmt.Errorf("no backup available to undo %s of %s", last.Type, last.Path)
		}
		content, err := os.ReadFile(last.Backup)
		if err != nil {
			return fmt.Errorf("reading backup: %w", err)
		}
		if err := writeFileAtomic(last.Path, content, fileMode(last.Path)); err != nil {
			return fmt.Errorf("restoring file: %w", err)
		}
		render.Success(fmt.Sprintf("Restored %s (undo %s)", last.Path, last.Type))
	}

	// Drop the undone entry from the journal.
	remaining := append(lines[:idx], lines[idx+1:]...)
	out := strings.Join(remaining, "\n")
	if out != "" {
		out += "\n"
	}
	_ = os.WriteFile(journalPath(), []byte(out), 0644)
	return nil
}

// backup snapshots a file before modification and returns the snapshot path
// (empty if the file doesn't exist). Old snapshots are pruned (C23).
func backup(path string) string {
	content, err := os.ReadFile(path)
	if err != nil {
		return "" // file doesn't exist, no backup needed
	}
	_ = os.MkdirAll(backupDir, 0755)

	safeName := strings.ReplaceAll(path, string(filepath.Separator), "__")
	timestamp := time.Now().Format("20060102_150405.000000")
	backupPath := filepath.Join(backupDir, fmt.Sprintf("%s_%s", timestamp, safeName))
	if err := os.WriteFile(backupPath, content, 0644); err != nil {
		return ""
	}
	pruneBackups()
	return backupPath
}

// pruneBackups keeps only the most recent maxBackupFiles snapshots.
func pruneBackups() {
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return
	}
	var snaps []os.DirEntry
	for _, e := range entries {
		if e.IsDir() || e.Name() == "journal.jsonl" {
			continue
		}
		snaps = append(snaps, e)
	}
	if len(snaps) <= maxBackupFiles {
		return
	}
	// Names are timestamp-prefixed, so lexical order ≈ chronological.
	sort.Slice(snaps, func(i, j int) bool { return snaps[i].Name() < snaps[j].Name() })
	for _, e := range snaps[:len(snaps)-maxBackupFiles] {
		_ = os.Remove(filepath.Join(backupDir, e.Name()))
	}
}

// writeFileAtomic writes data to a temp file in the same directory and renames
// it into place, so a crash mid-write can't corrupt the target (C21).
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if dir == "" {
		dir = "."
	}
	tmp, err := os.CreateTemp(dir, ".af-tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("writing temp file: %w", werr)
	}
	if cerr != nil {
		_ = os.Remove(tmpName)
		return cerr
	}
	_ = os.Chmod(tmpName, mode)
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("replacing file: %w", err)
	}
	return nil
}

// fileMode returns the file's current mode, or 0644 if it can't be determined
// (so edits preserve the executable bit on scripts, C21).
func fileMode(path string) os.FileMode {
	if info, err := os.Stat(path); err == nil {
		return info.Mode().Perm()
	}
	return 0644
}

// confirm asks the user for y/n confirmation via the shared stdin reader
// (PROPOSAL-010 C11 — no more fmt.Scanln racing the chat loop's reader).
func confirm(prompt string) bool {
	return render.Confirm(prompt)
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

// showDiff prints an accurate line-level diff (LCS-based) so the user confirms
// against real changes, not the old index-by-index compare that flagged
// everything after an insertion (PROPOSAL-010 C17).
func showDiff(oldStr, newStr string) {
	oldLines := strings.Split(oldStr, "\n")
	newLines := strings.Split(newStr, "\n")

	// LCS is O(n*m); cap huge files rather than allocate a giant table.
	if len(oldLines) > 2000 || len(newLines) > 2000 {
		fmt.Fprintf(os.Stderr, "  %s(file too large for inline diff: %d → %d lines)%s\n",
			render.Dim, len(oldLines), len(newLines), render.Reset)
		return
	}

	const maxShow = 60
	shown := 0
	for _, d := range diffLines(oldLines, newLines) {
		if d.kind == ' ' {
			continue // show only changes, keep the preview focused
		}
		if shown >= maxShow {
			fmt.Fprintf(os.Stderr, "  %s... (diff truncated)%s\n", render.Dim, render.Reset)
			break
		}
		if d.kind == '-' {
			fmt.Fprintf(os.Stderr, "  %s- %s%s\n", render.Red, d.text, render.Reset)
		} else {
			fmt.Fprintf(os.Stderr, "  %s+ %s%s\n", render.Green, d.text, render.Reset)
		}
		shown++
	}
}

type diffOp struct {
	kind byte // '-', '+', ' '
	text string
}

// diffLines computes a line diff via a standard LCS dynamic program + backtrace.
func diffLines(a, b []string) []diffOp {
	n, m := len(a), len(b)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	var ops []diffOp
	i, j := 0, 0
	for i < n && j < m {
		if a[i] == b[j] {
			ops = append(ops, diffOp{' ', a[i]})
			i, j = i+1, j+1
		} else if dp[i+1][j] >= dp[i][j+1] {
			ops = append(ops, diffOp{'-', a[i]})
			i++
		} else {
			ops = append(ops, diffOp{'+', b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, diffOp{'-', a[i]})
	}
	for ; j < m; j++ {
		ops = append(ops, diffOp{'+', b[j]})
	}
	return ops
}

func formatSize(b int64) string {
	if b < 1024 {
		return fmt.Sprintf("%d B", b)
	}
	return fmt.Sprintf("%.1f KB", float64(b)/1024)
}
