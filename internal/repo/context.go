package repo

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// BuildContext creates a structured context string from scan results for the AI agent.
func BuildContext(scan *ScanResult, focus string) string {
	var sb strings.Builder

	sb.WriteString("# Repository Analysis Context\n\n")

	// Project overview
	sb.WriteString(fmt.Sprintf("## Project: %s\n", scan.ProjectName))
	sb.WriteString(fmt.Sprintf("- Root: %s\n", scan.Root))
	sb.WriteString(fmt.Sprintf("- Total files: %d\n", scan.TotalFiles))
	sb.WriteString(fmt.Sprintf("- Total lines: %d\n", scan.TotalLines))
	sb.WriteString(fmt.Sprintf("- Total size: %s\n\n", formatBytes(scan.TotalSize)))

	// Languages
	sb.WriteString("## Languages\n")
	langs := sortedLanguages(scan.Languages)
	for _, l := range langs {
		pct := float64(l.stat.Lines) / float64(scan.TotalLines) * 100
		if scan.TotalLines == 0 {
			pct = 0
		}
		sb.WriteString(fmt.Sprintf("- %s: %d files, %d lines (%.0f%%)\n", l.name, l.stat.Files, l.stat.Lines, pct))
	}
	sb.WriteString("\n")

	// Frameworks
	if len(scan.Frameworks) > 0 {
		sb.WriteString("## Frameworks & Tools\n")
		for _, fw := range scan.Frameworks {
			sb.WriteString(fmt.Sprintf("- %s\n", fw))
		}
		sb.WriteString("\n")
	}

	// Key files
	if len(scan.KeyFiles) > 0 {
		sb.WriteString("## Key Configuration Files\n")
		for _, kf := range scan.KeyFiles {
			sb.WriteString(fmt.Sprintf("- %s\n", kf))
		}
		sb.WriteString("\n")
	}

	// Directory tree
	if scan.Tree != "" {
		sb.WriteString("## Directory Structure (top 2 levels)\n```\n")
		sb.WriteString(scan.Tree)
		sb.WriteString("\n```\n\n")
	}

	// Key file contents (package.json, go.mod, Dockerfile, etc.)
	sb.WriteString("## Key File Contents\n\n")
	contentFiles := []string{
		"package.json", "go.mod", "requirements.txt", "pyproject.toml",
		"Cargo.toml", "Dockerfile", "docker-compose.yml",
		"CLAUDE.md", "README.md",
	}
	for _, name := range contentFiles {
		path := filepath.Join(scan.Root, name)
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		text := string(content)
		// Truncate very large files
		if len(text) > 3000 {
			text = text[:3000] + "\n... (truncated)"
		}
		sb.WriteString(fmt.Sprintf("### %s\n```\n%s\n```\n\n", name, text))
	}

	// Git info
	gitInfo := getGitInfo(scan.Root)
	if gitInfo != "" {
		sb.WriteString("## Git Info\n")
		sb.WriteString(gitInfo)
		sb.WriteString("\n")
	}

	// Focus instructions
	if focus != "" {
		sb.WriteString(fmt.Sprintf("\n## Analysis Focus: %s\n", focus))
		switch strings.ToLower(focus) {
		case "security":
			sb.WriteString("Focus on: vulnerabilities, injection risks, auth issues, secrets in code, dependency CVEs, input validation, OWASP top 10.\n")
		case "performance":
			sb.WriteString("Focus on: bottlenecks, N+1 queries, memory leaks, unnecessary allocations, caching opportunities, async patterns, database indexing.\n")
		case "architecture":
			sb.WriteString("Focus on: code organization, separation of concerns, dependency management, coupling, design patterns, scalability, maintainability.\n")
		case "tests":
			sb.WriteString("Focus on: test coverage, missing tests, test quality, mocking strategies, edge cases, integration vs unit tests.\n")
		default:
			sb.WriteString(fmt.Sprintf("Focus on: %s\n", focus))
		}
	}

	return sb.String()
}

// BuildFileContext creates context for a specific file.
func BuildFileContext(filePath string, scan *ScanResult) string {
	var sb strings.Builder

	absPath, _ := filepath.Abs(filePath)
	relPath, _ := filepath.Rel(scan.Root, absPath)

	content, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Sprintf("Error reading file: %v", err)
	}

	lang := detectLanguage(absPath)
	lines := strings.Count(string(content), "\n") + 1

	sb.WriteString(fmt.Sprintf("# File: %s\n", relPath))
	sb.WriteString(fmt.Sprintf("- Language: %s\n", lang))
	sb.WriteString(fmt.Sprintf("- Lines: %d\n", lines))
	sb.WriteString(fmt.Sprintf("- Size: %s\n", formatBytes(int64(len(content)))))
	sb.WriteString(fmt.Sprintf("- Project: %s\n\n", scan.ProjectName))
	sb.WriteString("## Content\n```" + strings.ToLower(lang) + "\n")
	text := string(content)
	if len(text) > 50000 {
		text = text[:50000] + "\n... (truncated at 50,000 chars)"
	}
	sb.WriteString(text)
	sb.WriteString("\n```\n")

	return sb.String()
}

// BuildDiffContext creates context from git diff.
func BuildDiffContext(root string) string {
	var sb strings.Builder

	// Staged + unstaged changes
	diff := runGitCommand(root, "diff", "--stat")
	diffStaged := runGitCommand(root, "diff", "--staged", "--stat")
	fullDiff := runGitCommand(root, "diff")
	fullDiffStaged := runGitCommand(root, "diff", "--staged")

	sb.WriteString("# Code Review: Uncommitted Changes\n\n")

	if diff == "" && diffStaged == "" {
		sb.WriteString("No uncommitted changes found.\n")
		return sb.String()
	}

	if diffStaged != "" {
		sb.WriteString("## Staged Changes\n```\n")
		sb.WriteString(diffStaged)
		sb.WriteString("\n```\n\n")
		sb.WriteString("## Staged Diff\n```diff\n")
		if len(fullDiffStaged) > 30000 {
			fullDiffStaged = fullDiffStaged[:30000] + "\n... (truncated)"
		}
		sb.WriteString(fullDiffStaged)
		sb.WriteString("\n```\n\n")
	}

	if diff != "" {
		sb.WriteString("## Unstaged Changes\n```\n")
		sb.WriteString(diff)
		sb.WriteString("\n```\n\n")
		sb.WriteString("## Unstaged Diff\n```diff\n")
		if len(fullDiff) > 30000 {
			fullDiff = fullDiff[:30000] + "\n... (truncated)"
		}
		sb.WriteString(fullDiff)
		sb.WriteString("\n```\n\n")
	}

	return sb.String()
}

// getGitInfo returns git branch, recent commits, and status.
func getGitInfo(root string) string {
	var sb strings.Builder

	branch := runGitCommand(root, "branch", "--show-current")
	if branch != "" {
		sb.WriteString(fmt.Sprintf("- Branch: %s\n", strings.TrimSpace(branch)))
	}

	log := runGitCommand(root, "log", "--oneline", "-10")
	if log != "" {
		sb.WriteString("- Recent commits:\n```\n")
		sb.WriteString(log)
		sb.WriteString("\n```\n")
	}

	status := runGitCommand(root, "status", "--short")
	if status != "" {
		sb.WriteString(fmt.Sprintf("- Uncommitted changes: %d files\n", strings.Count(status, "\n")))
	}

	return sb.String()
}

func runGitCommand(root string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

type langEntry struct {
	name string
	stat LangStat
}

func sortedLanguages(langs map[string]LangStat) []langEntry {
	var entries []langEntry
	for name, stat := range langs {
		entries = append(entries, langEntry{name, stat})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].stat.Lines > entries[j].stat.Lines
	})
	return entries
}

func formatBytes(b int64) string {
	if b < 1024 {
		return fmt.Sprintf("%d B", b)
	}
	if b < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(b)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(b)/(1024*1024))
}
