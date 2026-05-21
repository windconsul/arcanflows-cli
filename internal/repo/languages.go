package repo

import (
	"os"
	"path/filepath"
	"strings"
)

// Language detection by file extension.
var extensionMap = map[string]string{
	".go":    "Go",
	".rs":    "Rust",
	".py":    "Python",
	".js":    "JavaScript",
	".jsx":   "JavaScript",
	".ts":    "TypeScript",
	".tsx":   "TypeScript",
	".java":  "Java",
	".kt":    "Kotlin",
	".rb":    "Ruby",
	".php":   "PHP",
	".cs":    "C#",
	".cpp":   "C++",
	".c":     "C",
	".h":     "C/C++ Header",
	".swift": "Swift",
	".dart":  "Dart",
	".lua":   "Lua",
	".r":     "R",
	".scala": "Scala",
	".ex":    "Elixir",
	".exs":   "Elixir",
	".erl":   "Erlang",
	".zig":   "Zig",
	".v":     "V",
	".nim":   "Nim",

	// Web
	".html":  "HTML",
	".htm":   "HTML",
	".css":   "CSS",
	".scss":  "SCSS",
	".less":  "Less",
	".vue":   "Vue",
	".svelte": "Svelte",

	// Config/Data
	".json":  "JSON",
	".yaml":  "YAML",
	".yml":   "YAML",
	".toml":  "TOML",
	".xml":   "XML",
	".sql":   "SQL",
	".graphql": "GraphQL",
	".gql":    "GraphQL",
	".proto":  "Protocol Buffers",

	// Shell/Script
	".sh":   "Shell",
	".bash": "Shell",
	".zsh":  "Shell",
	".ps1":  "PowerShell",
	".bat":  "Batch",

	// Docs
	".md":   "Markdown",
	".mdx":  "MDX",
	".rst":  "reStructuredText",
	".tex":  "LaTeX",

	// Infrastructure
	".tf":         "Terraform",
	".tfvars":     "Terraform",
	".dockerfile": "Docker",
	".hcl":        "HCL",
}

// Special filenames that indicate language/framework.
var specialFiles = map[string]string{
	"Dockerfile":       "Docker",
	"docker-compose.yml": "Docker Compose",
	"docker-compose.yaml": "Docker Compose",
	"Makefile":         "Make",
	"CMakeLists.txt":   "CMake",
	"Vagrantfile":      "Ruby",
	"Rakefile":         "Ruby",
	"Gemfile":          "Ruby",
	"Procfile":         "Heroku",
	"Caddyfile":        "Caddy",
	".env":             "Environment",
	".env.example":     "Environment",
}

// DetectLanguageForFile is the exported version for external callers.
func DetectLanguageForFile(path string) string {
	return detectLanguage(path)
}

// detectLanguage returns the language for a file path.
func detectLanguage(path string) string {
	name := filepath.Base(path)

	// Check special filenames first
	if lang, ok := specialFiles[name]; ok {
		return lang
	}

	ext := strings.ToLower(filepath.Ext(path))
	if lang, ok := extensionMap[ext]; ok {
		return lang
	}

	return ""
}

// Framework detection rules.
type frameworkRule struct {
	Name      string
	Indicator string // file that indicates this framework
	Language  string
}

var frameworkRules = []frameworkRule{
	// JavaScript/TypeScript
	{Name: "Next.js", Indicator: "next.config.js", Language: "TypeScript"},
	{Name: "Next.js", Indicator: "next.config.mjs", Language: "TypeScript"},
	{Name: "Next.js", Indicator: "next.config.ts", Language: "TypeScript"},
	{Name: "React", Indicator: "src/App.tsx", Language: "TypeScript"},
	{Name: "React", Indicator: "src/App.jsx", Language: "JavaScript"},
	{Name: "Vue.js", Indicator: "vue.config.js", Language: "JavaScript"},
	{Name: "Vue.js", Indicator: "nuxt.config.ts", Language: "TypeScript"},
	{Name: "Angular", Indicator: "angular.json", Language: "TypeScript"},
	{Name: "Svelte", Indicator: "svelte.config.js", Language: "JavaScript"},
	{Name: "Express", Indicator: "node_modules/express", Language: "JavaScript"},

	// Go
	{Name: "Gin", Indicator: "go.sum", Language: "Go"}, // checked via go.mod content
	{Name: "Echo", Indicator: "go.sum", Language: "Go"},

	// Python
	{Name: "FastAPI", Indicator: "requirements.txt", Language: "Python"},
	{Name: "Django", Indicator: "manage.py", Language: "Python"},
	{Name: "Flask", Indicator: "requirements.txt", Language: "Python"},

	// Ruby
	{Name: "Rails", Indicator: "config/routes.rb", Language: "Ruby"},

	// Java
	{Name: "Spring Boot", Indicator: "pom.xml", Language: "Java"},
	{Name: "Spring Boot", Indicator: "build.gradle", Language: "Java"},

	// Rust
	{Name: "Actix", Indicator: "Cargo.toml", Language: "Rust"},

	// Infrastructure
	{Name: "Docker", Indicator: "Dockerfile", Language: "Docker"},
	{Name: "Docker Compose", Indicator: "docker-compose.yml", Language: "Docker"},
	{Name: "Terraform", Indicator: "main.tf", Language: "Terraform"},
	{Name: "GitHub Actions", Indicator: ".github/workflows", Language: "CI/CD"},
}

// detectFrameworks checks for framework indicators in the project root.
func detectFrameworks(root string) []string {
	found := make(map[string]bool)

	for _, rule := range frameworkRules {
		path := filepath.Join(root, rule.Indicator)
		if _, err := os.Stat(path); err == nil {
			found[rule.Name] = true
		}
	}

	// Deep checks via file content
	goMod := filepath.Join(root, "go.mod")
	if content, err := os.ReadFile(goMod); err == nil {
		s := string(content)
		if strings.Contains(s, "gin-gonic/gin") {
			found["Gin"] = true
		}
		if strings.Contains(s, "labstack/echo") {
			found["Echo"] = true
		}
		if strings.Contains(s, "gofiber/fiber") {
			found["Fiber"] = true
		}
	}

	pkgJSON := filepath.Join(root, "package.json")
	if content, err := os.ReadFile(pkgJSON); err == nil {
		s := string(content)
		if strings.Contains(s, `"next"`) {
			found["Next.js"] = true
		}
		if strings.Contains(s, `"react"`) {
			found["React"] = true
		}
		if strings.Contains(s, `"vue"`) {
			found["Vue.js"] = true
		}
		if strings.Contains(s, `"tailwindcss"`) {
			found["Tailwind CSS"] = true
		}
		if strings.Contains(s, `"express"`) {
			found["Express"] = true
		}
	}

	reqTxt := filepath.Join(root, "requirements.txt")
	if content, err := os.ReadFile(reqTxt); err == nil {
		s := strings.ToLower(string(content))
		if strings.Contains(s, "fastapi") {
			found["FastAPI"] = true
		}
		if strings.Contains(s, "django") {
			found["Django"] = true
		}
		if strings.Contains(s, "flask") {
			found["Flask"] = true
		}
	}

	var result []string
	for fw := range found {
		result = append(result, fw)
	}
	return result
}

// Key files that provide project context.
var keyFileNames = []string{
	"package.json", "go.mod", "requirements.txt", "pyproject.toml",
	"Cargo.toml", "pom.xml", "build.gradle", "Gemfile",
	"Dockerfile", "docker-compose.yml", "docker-compose.yaml",
	"Makefile", "Caddyfile", ".env.example",
	"tsconfig.json", "tailwind.config.js", "tailwind.config.ts",
	"next.config.js", "next.config.mjs", "next.config.ts",
	"vite.config.ts", "webpack.config.js",
	"README.md", "CHANGELOG.md", "CLAUDE.md",
	".github/workflows/build.yml", ".github/workflows/deploy.yml",
	".github/workflows/ci.yml", ".github/workflows/test.yml",
}

// findKeyFiles returns which key files exist in the project.
func findKeyFiles(root string) []string {
	var found []string
	for _, name := range keyFileNames {
		path := filepath.Join(root, name)
		if _, err := os.Stat(path); err == nil {
			found = append(found, name)
		}
	}
	return found
}
