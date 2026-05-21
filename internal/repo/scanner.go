package repo

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileInfo holds metadata about a scanned file.
type FileInfo struct {
	Path     string `json:"path"`
	RelPath  string `json:"rel_path"`
	Size     int64  `json:"size"`
	Language string `json:"language,omitempty"`
	Lines    int    `json:"lines,omitempty"`
}

// ScanResult holds the full result of a directory scan.
type ScanResult struct {
	Root         string              `json:"root"`
	ProjectName  string              `json:"project_name"`
	TotalFiles   int                 `json:"total_files"`
	TotalLines   int                 `json:"total_lines"`
	TotalSize    int64               `json:"total_size"`
	Languages    map[string]LangStat `json:"languages"`
	Frameworks   []string            `json:"frameworks"`
	KeyFiles     []string            `json:"key_files"`
	Tree         string              `json:"tree"`
	Files        []FileInfo          `json:"files"`
}

// LangStat holds stats for a detected language.
type LangStat struct {
	Files int   `json:"files"`
	Lines int   `json:"lines"`
	Bytes int64 `json:"bytes"`
}

// Default patterns to exclude from scanning.
var defaultExcludes = []string{
	"node_modules", ".git", "vendor", "dist", ".next", "build", "target",
	"__pycache__", ".venv", "venv", ".tox", ".mypy_cache",
	".idea", ".vscode", ".DS_Store", "coverage", ".nyc_output",
	"*.min.js", "*.min.css", "*.map", "*.lock", "package-lock.json",
	"yarn.lock", "pnpm-lock.yaml", "go.sum", "Pipfile.lock", "poetry.lock",
	"*.wasm", "*.so", "*.dylib", "*.dll", "*.exe", "*.bin",
	"*.png", "*.jpg", "*.jpeg", "*.gif", "*.svg", "*.ico", "*.webp",
	"*.mp3", "*.mp4", "*.wav", "*.avi", "*.mov",
	"*.pdf", "*.zip", "*.tar", "*.gz", "*.rar",
	"*.pyc", "*.pyo", "*.class", "*.o", "*.a",
}

// Scanner scans a directory for source files.
type Scanner struct {
	Root        string
	MaxFiles    int
	MaxFileSize int64
	Excludes    []string
}

// NewScanner creates a scanner with defaults.
func NewScanner(root string, maxFiles int, maxFileSize int64, extraExcludes []string) *Scanner {
	excludes := append(defaultExcludes, extraExcludes...)
	return &Scanner{
		Root:        root,
		MaxFiles:    maxFiles,
		MaxFileSize: maxFileSize,
		Excludes:    excludes,
	}
}

// Scan walks the directory and collects file metadata.
func (s *Scanner) Scan() (*ScanResult, error) {
	absRoot, err := filepath.Abs(s.Root)
	if err != nil {
		return nil, err
	}

	// Load .gitignore patterns
	gitignorePatterns := loadGitignore(absRoot)

	result := &ScanResult{
		Root:        absRoot,
		ProjectName: filepath.Base(absRoot),
		Languages:   make(map[string]LangStat),
	}

	var treeLines []string
	fileCount := 0

	err = filepath.Walk(absRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors
		}

		relPath, _ := filepath.Rel(absRoot, path)
		if relPath == "." {
			return nil
		}

		// Check excludes
		if s.shouldExclude(relPath, info.IsDir(), gitignorePatterns) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if info.IsDir() {
			// Add to tree (only top 2 levels)
			depth := strings.Count(relPath, string(filepath.Separator))
			if depth <= 2 {
				indent := strings.Repeat("  ", depth)
				treeLines = append(treeLines, fmt.Sprintf("%s%s/", indent, info.Name()))
			}
			return nil
		}

		// File
		if fileCount >= s.MaxFiles {
			return nil
		}
		if info.Size() > s.MaxFileSize {
			return nil
		}

		lang := detectLanguage(path)
		lines := 0
		if lang != "" && info.Size() < 500000 { // count lines for files under 500KB
			lines = countLines(path)
		}

		fi := FileInfo{
			Path:     path,
			RelPath:  relPath,
			Size:     info.Size(),
			Language: lang,
			Lines:    lines,
		}
		result.Files = append(result.Files, fi)
		result.TotalSize += info.Size()
		result.TotalLines += lines
		fileCount++

		// Update language stats
		if lang != "" {
			stat := result.Languages[lang]
			stat.Files++
			stat.Lines += lines
			stat.Bytes += info.Size()
			result.Languages[lang] = stat
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	result.TotalFiles = fileCount
	result.Tree = strings.Join(treeLines, "\n")
	result.KeyFiles = findKeyFiles(absRoot)
	result.Frameworks = detectFrameworks(absRoot)

	return result, nil
}

// shouldExclude checks if a path matches exclude patterns.
func (s *Scanner) shouldExclude(relPath string, isDir bool, gitignore []string) bool {
	name := filepath.Base(relPath)

	for _, pattern := range s.Excludes {
		if strings.HasPrefix(pattern, "*.") {
			// Extension match
			ext := strings.TrimPrefix(pattern, "*")
			if strings.HasSuffix(name, ext) {
				return true
			}
		} else if name == pattern || relPath == pattern {
			return true
		}
	}

	// Check gitignore
	for _, pattern := range gitignore {
		matched, _ := filepath.Match(pattern, name)
		if matched {
			return true
		}
		if isDir && strings.HasSuffix(pattern, "/") {
			if name == strings.TrimSuffix(pattern, "/") {
				return true
			}
		}
	}

	return false
}

// loadGitignore reads .gitignore patterns from the root.
func loadGitignore(root string) []string {
	var patterns []string
	file, err := os.Open(filepath.Join(root, ".gitignore"))
	if err != nil {
		return patterns
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	return patterns
}

// countLines counts newlines in a file.
func countLines(path string) int {
	file, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer file.Close()

	count := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		count++
	}
	return count
}
