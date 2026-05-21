package repo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Instruction file names in priority order.
var instructionFiles = []string{
	".arcanflows.md",
	"ARCANFLOWS.md",
	".arcanflows/instructions.md",
}

// LoadInstructions searches for an ARCANFLOWS.md file starting from dir
// and walking up to the git root or home directory.
// Returns the content and the path where it was found.
func LoadInstructions(dir string) (string, string, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", "", err
	}

	home, _ := os.UserHomeDir()
	var allContent []string
	var foundPath string

	current := absDir
	for {
		for _, name := range instructionFiles {
			path := filepath.Join(current, name)
			content, err := os.ReadFile(path)
			if err == nil && len(content) > 0 {
				allContent = append(allContent, string(content))
				if foundPath == "" {
					foundPath = path
				}
			}
		}

		// Stop at git root
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
			break
		}

		// Stop at home directory
		if current == home || current == "/" {
			break
		}

		// Walk up
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}

	if len(allContent) == 0 {
		return "", "", nil
	}

	// Merge: child content takes priority (first found)
	// But include all for completeness
	merged := strings.Join(allContent, "\n\n---\n\n")
	return merged, foundPath, nil
}

// GenerateTemplate scans a project and generates a starter ARCANFLOWS.md.
func GenerateTemplate(scan *ScanResult) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# Project: %s\n\n", scan.ProjectName))

	// About
	sb.WriteString("## About\n")
	sb.WriteString(fmt.Sprintf("<!-- Describe your project here -->\n"))
	sb.WriteString(fmt.Sprintf("%s is a ", scan.ProjectName))

	// Detect primary language
	var primaryLang string
	var maxLines int
	for lang, stat := range scan.Languages {
		if stat.Lines > maxLines {
			maxLines = stat.Lines
			primaryLang = lang
		}
	}
	if primaryLang != "" {
		sb.WriteString(fmt.Sprintf("%s project", primaryLang))
	} else {
		sb.WriteString("software project")
	}
	if len(scan.Frameworks) > 0 {
		sb.WriteString(fmt.Sprintf(" built with %s", strings.Join(scan.Frameworks, ", ")))
	}
	sb.WriteString(".\n\n")

	// Tech Stack
	sb.WriteString("## Tech Stack\n")
	for lang, stat := range scan.Languages {
		if stat.Files > 1 {
			sb.WriteString(fmt.Sprintf("- %s: %d files, %d lines\n", lang, stat.Files, stat.Lines))
		}
	}
	if len(scan.Frameworks) > 0 {
		sb.WriteString(fmt.Sprintf("- Frameworks: %s\n", strings.Join(scan.Frameworks, ", ")))
	}
	sb.WriteString("\n")

	// Architecture
	sb.WriteString("## Architecture\n")
	if scan.Tree != "" {
		lines := strings.Split(scan.Tree, "\n")
		for _, line := range lines {
			if strings.Count(line, "  ") <= 1 && strings.HasSuffix(line, "/") {
				dir := strings.TrimSpace(strings.TrimSuffix(line, "/"))
				sb.WriteString(fmt.Sprintf("- %s/ -- <!-- describe this directory -->\n", dir))
			}
		}
	}
	sb.WriteString("\n")

	// Conventions
	sb.WriteString("## Conventions\n")
	sb.WriteString("<!-- Add your coding conventions here -->\n")
	switch primaryLang {
	case "Go":
		sb.WriteString("- Follow Go conventions (gofmt, golint)\n")
		sb.WriteString("- Error handling: always check errors, wrap with context\n")
	case "TypeScript", "JavaScript":
		sb.WriteString("- Use TypeScript strict mode\n")
		sb.WriteString("- Prefer functional components in React\n")
	case "Python":
		sb.WriteString("- Follow PEP 8, use type hints\n")
		sb.WriteString("- Use async/await for I/O operations\n")
	}
	sb.WriteString("- Write tests for new features\n")
	sb.WriteString("- Conventional commits (feat:, fix:, docs:)\n")
	sb.WriteString("\n")

	// Current Focus
	sb.WriteString("## Current Focus\n")
	sb.WriteString("<!-- What are you working on right now? -->\n")
	sb.WriteString("- \n\n")

	// Rules
	sb.WriteString("## Rules\n")
	sb.WriteString("- NEVER modify .env files or files containing secrets\n")
	sb.WriteString("- ALWAYS run tests before suggesting commits\n")
	sb.WriteString("- Keep new files under 300 lines\n")
	sb.WriteString("\n")

	// Do Not Touch
	sb.WriteString("## Do Not Touch\n")
	sb.WriteString("- .env\n")
	sb.WriteString("- *.key\n")
	sb.WriteString("- *.pem\n")

	return sb.String()
}
