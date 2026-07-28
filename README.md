# ArcanFlows CLI

**AI Automation from your terminal.** Chat with AI agents, manage workflows, query data tables, analyze repositories, and automate your development workflow — all from the command line.

## Installation

### Quick Install (Linux / macOS)

```bash
curl -fsSL https://raw.githubusercontent.com/windconsul/arcanflows-cli/main/install.sh | sh
```

### Homebrew (macOS)

```bash
brew install arcanflows/tap/arcanflows
```

### Go Install

```bash
go install github.com/arcanflows/cli@latest
```

### Download Binary

Download the binary for your platform from [Releases](https://github.com/windconsul/ai-saas-agents/releases):

| Platform | Architecture | Binary |
|----------|-------------|--------|
| macOS | Apple Silicon (M1/M2/M3) | `arcanflows-darwin-arm64` |
| macOS | Intel | `arcanflows-darwin-amd64` |
| Linux | x86_64 | `arcanflows-linux-amd64` |
| Linux | ARM64 | `arcanflows-linux-arm64` |
| Windows | x86_64 | `arcanflows-windows-amd64.exe` |

```bash
chmod +x arcanflows-*
sudo mv arcanflows-* /usr/local/bin/arcanflows
```

### Build from Source

```bash
cd cli/
make build          # Build for current platform
make all            # Build for all platforms
make install        # Install to GOPATH/bin
```

---

## Quick Start

```bash
# 1. Log in
arcanflows login

# 2. Start chatting with an AI agent
arcanflows chat

# 3. Initialize project instructions
arcanflows init

# 4. Analyze your codebase
arcanflows repo analyze
```

---

## Authentication

### Email / Password

```bash
arcanflows login
# Enter email and password interactively
```

### API Key

```bash
arcanflows login --api-key YOUR_API_KEY

# Or via environment variable
export ARCANFLOWS_API_KEY=your-api-key
arcanflows chat
```

### Server Configuration

```bash
# Custom server
arcanflows login --server https://your-instance.arcanflows.com

# Or via environment variable
export ARCANFLOWS_SERVER=https://your-instance.arcanflows.com
```

### Session Management

```bash
arcanflows whoami     # Show current user and organization
arcanflows logout     # Clear stored credentials
```

Credentials are stored securely in `~/.arcanflows/credentials.yaml` with restricted file permissions (0600).

---

## Commands

### Chat

Interactive streaming chat with AI agents. Supports file operations, shell execution, and session persistence.

```bash
arcanflows chat                           # Interactive picker
arcanflows chat --agent code-reviewer     # By name or slug
arcanflows chat --agent-id UUID           # By ID
arcanflows chat --continue                # Resume last session
arcanflows chat --model gpt-4o            # Override model
```

#### In-Session Commands

| Command | Action |
|---------|--------|
| `/help` | Show all commands |
| `/file <path>` | Read and attach a file to context |
| `/create <path>` | Ask agent to create a file |
| `/run <command>` | Execute a shell command |
| `/diff` | Show git diff |
| `/status` | Show git status |
| `/tree` | Show directory tree |
| `/undo` | Undo last file operation |
| `/context` | Show loaded project instructions and memory |
| `/sessions` | Show current session info |
| `/history` | Show files modified and commands run this session |
| `/hooks` | Show configured hooks |
| `/clear` | Clear conversation |
| `/exit` | Exit chat |

### Builder

Design and create no-code components — data tables, forms, agents, workflows, tools, analytics dashboards, and whole apps — by conversation. The Builder proposes a plan; you review it and type `/build` to create everything, cross-wired.

```bash
arcanflows builder                                              # Interactive
arcanflows builder "a contacts table and an intake form that saves to it"
arcanflows builder "a daily sales report dashboard" --yes       # plan + build, non-interactive
```

Refine by just talking — *"add a status field"*, *"make the form shorter"*, *"also notify the agent"*. Nothing is created until you run `/build`.

#### In-Session Commands

| Command | Action |
|---------|--------|
| `/build` | Create the current plan (cross-wired) |
| `/plan` | Show the current (un-built) plan |
| `/new` | Start a fresh Builder project |
| `/help` | Show all commands |
| `/exit` | Exit the Builder |

After a build, each created component shows a status: green ✓ when ready, yellow `!` with the specific fix when it needs attention (e.g. an empty dashboard or an unmapped form).

#### Dev Mode Features

The chat agent can create, edit, and delete files in your project:

```
--- Create file: src/utils/helper.ts ---
export function formatDate(d: Date): string { ... }
--- End file ---
```

For edits to existing files, the agent can send a **targeted search/replace** instead of rewriting the whole file — each `SEARCH` must match exactly one place (a not-found or ambiguous match is reported back so the agent can retry):

```
--- Replace in: src/utils/helper.ts ---
<<<<<<< SEARCH
return d.toISOString()
=======
return d.toISOString().slice(0, 10)
>>>>>>> REPLACE
--- End replace ---
```

Edits are applied atomically (temp file + rename), preserve the file's permissions, and are shown as an accurate diff before you approve. `/undo` reverses the last operation precisely (including undoing a newly-created file) via an operation journal in `.arcanflows/backups/`.

The agent can also suggest shell commands:

```
EXECUTE: npm install express
```

**Agentic loop:** after you approve and the CLI applies file edits or runs commands, their results (exit codes, output, errors) are sent back to the agent automatically, and it continues — so it can build, see a failure, and fix it within a single turn. The loop is bounded by `chat_max_tool_iterations` (default 8) and stops as soon as the agent has no more actions. Press **Ctrl+C** to cancel the current turn and return to the prompt.

Both require confirmation before execution. File operations are **confined to the project directory** by default (use `--allow-outside` to override), and protected paths (`.env*`, `*.key`, `*.pem`, `*.p8`, `id_rsa`, `credentials*`, `secrets*`, the `.git` tree, …) are never modified. The confirmation prompt is the real safety boundary — review each action before approving.

### Ask

Send a single question (non-interactive). Useful for piping and scripting.

```bash
arcanflows ask "What is the time complexity of quicksort?"
echo "Explain this error" | arcanflows ask
git diff | arcanflows ask "Review this diff"
arcanflows ask --agent security-expert "Check this for vulnerabilities"
```

### Agents

```bash
arcanflows agents list                          # List all agents
arcanflows agents get <id>                      # Get agent details
arcanflows agents create --name "My Agent" \
  --model gpt-4o --temperature 0.7              # Create agent
arcanflows agents delete <id>                   # Delete agent
```

Create flags: `--name`, `--model`, `--system-prompt`, `--system-prompt-file`, `--temperature`, `--max-tokens`, `--language`

### Workflows

```bash
arcanflows workflows list                       # List all workflows
arcanflows workflows list --status published    # Filter by status
arcanflows workflows get <id>                   # Get workflow details
arcanflows workflows run <id>                   # Trigger execution
arcanflows workflows run <id> --data '{"key":"value"}'
arcanflows workflows run <id> --data-file input.json
arcanflows workflows executions <id>            # View execution history
arcanflows workflows publish <id>               # Publish a workflow
```

### Data Tables

```bash
arcanflows tables list                          # List all tables
arcanflows tables schema <id>                   # Show table fields
arcanflows tables query <id>                    # Query records
arcanflows tables query <id> --filter status=active --limit 20
arcanflows tables insert <id> --data '{"name":"John","email":"john@example.com"}'
arcanflows tables export <id>                   # Export as CSV
arcanflows tables export <id> --format json     # Export as JSON
```

Aliases: `dt`, `data-tables`

### Forms

```bash
arcanflows forms list                           # List all forms
arcanflows forms get <id>                       # Get form details
arcanflows forms submissions <form-id>          # View submissions
arcanflows forms submissions <form-id> --last 5
arcanflows forms submit --slug my-form --data '{"name":"John"}'
```

### Tools

```bash
arcanflows tools list                           # List available tools
arcanflows tools exec send-email --input '{"to":"user@example.com","subject":"Hello"}'
```

### Repository Analysis

AI-powered analysis of code repositories. Automatically detects languages, frameworks, and project structure.

```bash
arcanflows repo analyze                         # Full analysis
arcanflows repo analyze --focus security        # Focused analysis
arcanflows repo review                          # Review uncommitted changes
arcanflows repo review src/auth.go              # Review specific file
arcanflows repo explain src/utils/parser.ts     # Explain a file
arcanflows repo audit                           # Security audit
arcanflows repo audit --focus sql-injection
arcanflows repo test src/services/user.go       # Generate unit tests
arcanflows repo test src/api.ts --framework jest
arcanflows repo docs                            # Generate documentation
```

Focus options: `security`, `performance`, `architecture`, `tests`
Test frameworks: `jest`, `vitest`, `go-test`, `pytest`

**Supported Languages:** Go, Rust, Python, JavaScript, TypeScript, Java, Kotlin, Ruby, PHP, C#, C++, C, Swift, Dart, Lua, R, Scala, Elixir, Erlang, Zig, V, Nim, HTML, CSS, SCSS, Vue, Svelte, JSON, YAML, TOML, SQL, GraphQL, Protocol Buffers, Shell, Terraform, Docker, and more (60+ file extensions).

**Framework Detection:** Next.js, React, Vue, Angular, Express, Django, Flask, Rails, Spring Boot, Gin, Echo, Fiber, Docker, GitHub Actions, Terraform, and 20+ others.

### Organizations

Multi-tenant support for users with access to multiple organizations.

```bash
arcanflows org list                             # List organizations
arcanflows org current                          # Show current org
arcanflows org switch "My Company"              # Switch by name
arcanflows org switch <tenant-id>               # Switch by ID
```

Aliases: `tenant`, `organization`

---

## Project Instructions (ARCANFLOWS.md)

Create an `ARCANFLOWS.md` file in your project root to give the CLI project-specific context. This file is automatically loaded and included in every prompt.

```bash
arcanflows init                     # Auto-generate from project scan
arcanflows init --force             # Overwrite existing
```

Example `ARCANFLOWS.md`:

```markdown
# Project: MyApp
## Stack
- Next.js 14 + TypeScript
- PostgreSQL + Prisma ORM

## Conventions
- Use kebab-case for file names
- All API routes in src/app/api/

## Current Focus
- Migrating from REST to tRPC
```

---

## Context Memory

Persistent facts about your project, loaded into every prompt automatically.

```bash
arcanflows context add "Redis is on port 6380, not default"
arcanflows context add "We use pytest with async fixtures"
arcanflows context list
arcanflows context remove 1
arcanflows context clear
```

Aliases: `ctx`, `memory`

Memory is stored in `.arcanflows/memory/project.yaml`.

---

## Session History

Chat sessions are automatically saved and can be resumed across terminal sessions.

```bash
arcanflows chat --continue                      # Resume last session
arcanflows sessions list                        # List saved sessions
arcanflows sessions resume 1                    # Resume by number
arcanflows sessions show 1                      # View session details
arcanflows sessions delete 1                    # Delete a session
arcanflows sessions clear                       # Clear all sessions
```

Sessions track: messages exchanged, files modified, commands run, agent used, timestamps.
Auto-cleanup after 30 days (configurable via `session_max_age_days`).

Aliases: `history`

---

## Hooks

Automate tasks before and after CLI operations with hooks defined in `.arcanflows/hooks.yaml`.

```bash
arcanflows hooks init               # Create starter hooks.yaml
arcanflows hooks list               # Show configured hooks
arcanflows hooks test post_edit     # Test-run hooks for an event
```

### Example hooks.yaml

```yaml
pre_chat:
  - command: "git status --short"
    description: "Include git status in context"

post_edit:
  - command: "prettier --write {{file}}"
    description: "Auto-format after edits"
  - command: "eslint {{file}}"
    description: "Lint after edits"
    continue_on: error

post_create:
  - command: "git add {{file}}"
    description: "Auto-stage new files"

pre_commit:
  - command: "npm test"
    description: "Run tests before commit"
```

### Hook Events

| Event | Fires When |
|-------|------------|
| `pre_chat` | Before chat session starts (output injected into context) |
| `post_chat` | After chat session ends |
| `pre_edit` / `post_edit` | Before/after agent edits a file (a failing `pre_edit` blocks the edit) |
| `pre_create` / `post_create` | Before/after agent creates a file (a failing `pre_create` blocks it) |
| `pre_delete` / `post_delete` | Before/after agent deletes a file (a failing `pre_delete` blocks it) |
| `pre_run` / `post_run` | Before/after shell command execution |

### Template Variables

| Variable | Value |
|----------|-------|
| `{{file}}` | Path of file being operated on |
| `{{project_root}}` | Project root directory |
| `{{branch}}` | Current git branch |
| `{{agent}}` | Current agent name |

Hook options:
- `continue_on: error` — Continue even if the hook fails
- `silent: true` — Suppress hook output

---

## Configuration

### Profiles

Manage multiple server connections with profiles.

```bash
arcanflows config view                          # Show config
arcanflows config set server https://api.example.com
arcanflows config set default-agent code-reviewer
arcanflows config set output-format json

arcanflows config profile list                  # List profiles
arcanflows config profile create staging --server https://staging-api.arcanflows.com
arcanflows config profile use staging           # Switch profile
```

### Config File

Located at `~/.arcanflows/config.yaml`:

```yaml
version: 1
active_profile: default
profiles:
  default:
    server: https://api.arcanflows.com
    organization: "My Company"
    default_agent: code-reviewer
    output_format: table               # table, json, yaml, csv
settings:
  chat_streaming: true
  chat_markdown: true
  chat_syntax_highlight: true
  chat_max_history: 50
  chat_max_tool_iterations: 8       # agent-loop cap per turn (file/shell → results → continue)
  repo_max_file_size: 100000           # bytes
  repo_max_files: 200
  repo_exclude_patterns:
    - node_modules
    - .git
    - vendor
    - dist
  color: auto                          # auto, always, never
  timeout: 30s
  retry_count: 3
  session_history: true
  session_max_age_days: 30
```

### Environment Variables

| Variable | Overrides |
|----------|-----------|
| `ARCANFLOWS_API_KEY` | API key authentication |
| `ARCANFLOWS_SERVER` | Server URL |
| `ARCANFLOWS_CONFIG_DIR` | Config directory (default: `~/.arcanflows`) |

---

## Doctor

Diagnose your CLI setup in one command — config, credentials, token expiry, server reachability, whether the server actually accepts your credentials, and CLI version currency.

```bash
arcanflows doctor          # report the status of every check (read-only)
arcanflows doctor --fix    # report, then apply safe fixes (refresh token, self-update)
```

Read-only by default: it prints a ✓/!/✗ checklist and, for each issue, the exact command to fix it. With `--fix` it applies the safe fixes automatically (refresh an expired token via your refresh token, and install the latest CLI version).

## Auto-Update

Updates pull from the public CLI repo's GitHub releases (`windconsul/arcanflows-cli`; override with `ARCANFLOWS_CLI_REPO`).

```bash
arcanflows update check             # Check for new version
arcanflows update                   # Download and install latest
```

---

## Shell Completions

```bash
# Bash (Linux)
arcanflows completion bash | sudo tee /etc/bash_completion.d/arcanflows > /dev/null

# Bash (macOS with bash-completion)
arcanflows completion bash > $(brew --prefix)/etc/bash_completion.d/arcanflows

# Zsh
arcanflows completion zsh > "${fpath[1]}/_arcanflows"

# Fish
arcanflows completion fish > ~/.config/fish/completions/arcanflows.fish

# PowerShell
arcanflows completion powershell | Out-String | Invoke-Expression
```

---

## CI/CD Integration

### GitHub Actions

```yaml
name: ArcanFlows Code Review
on: [pull_request]
jobs:
  review:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Install ArcanFlows CLI
        run: curl -fsSL https://raw.githubusercontent.com/windconsul/arcanflows-cli/main/install.sh | sh
      - name: Run Code Review
        env:
          ARCANFLOWS_API_KEY: ${{ secrets.ARCANFLOWS_API_KEY }}
          ARCANFLOWS_SERVER: ${{ secrets.ARCANFLOWS_SERVER }}
        run: arcanflows repo review --format json
```

### Docker

```bash
docker run -it \
  -e ARCANFLOWS_API_KEY=your-key \
  -e ARCANFLOWS_SERVER=https://api.arcanflows.com \
  -v $(pwd):/workspace \
  ghcr.io/arcanflows/cli chat
```

---

## File Structure

```
~/.arcanflows/
  config.yaml              # CLI configuration
  credentials.yaml         # Auth tokens (0600 permissions)
  history/
    sessions/              # Saved chat sessions

.arcanflows/               # Per-project directory
  memory/
    project.yaml           # Persistent context entries
  hooks.yaml               # Pre/post command hooks
  backups/                 # File backups before edits

ARCANFLOWS.md              # Project instructions (auto-loaded)
```

---

## Admin Guide

### Server Requirements

The ArcanFlows CLI connects to an ArcanFlows Platform instance (self-hosted or cloud). The backend requires:

- **Agent Manager** service running (Go, port 8080)
- **RAG Service** (Python, for document extraction and knowledge base)
- PostgreSQL, Redis, MinIO (for storage)
- Valid JWT or API key for authentication

### Multi-Tenant Setup

Users with access to multiple tenants can switch between them:

```bash
arcanflows org list
arcanflows org switch "Production Tenant"
```

The CLI stores the active tenant in the profile configuration. All API calls are scoped to the active tenant.

### API Key Management

For CI/CD and automation, create API keys from the ArcanFlows Console:

1. Go to **Settings > API Keys**
2. Create a new key with appropriate permissions
3. Use via `--api-key` flag or `ARCANFLOWS_API_KEY` environment variable

API keys are scoped to a single tenant and inherit the creating user's permissions.

### Security Considerations

- Credentials stored in `~/.arcanflows/credentials.yaml` with 0600 permissions
- API keys never logged or displayed after creation
- **Shell execution requires explicit user confirmation** — this is the primary safety control
- Agent file operations are **confined to the project directory** (`../`, absolute paths, and symlink escapes are rejected unless `--allow-outside` is passed)
- Protected paths (`.env*`, `*.key`, `*.pem`, `*.p8`, `id_rsa`/`id_ed25519`, `credentials*`, `secrets*`, `.npmrc`, `.netrc`, the `.git` tree) cannot be modified, deleted, or moved by agents
- Blocked commands (`rm -rf /`, `shutdown`, etc.) are a **best-effort speed bump, not a security boundary** — a creative command can bypass the substring blocklist, so the confirmation prompt is what actually protects you. Never run with auto-confirm on untrusted input.
- Command and hook output is capped (1 MiB) so a runaway process can't exhaust memory
- Before `repo analyze`/`review`/`audit` upload file contents for analysis, a best-effort **secret-redaction pass** replaces detected keys/tokens/private-key blocks with `[REDACTED]` (on top of `.gitignore`); the count of redactions is reported
- Hook commands run with a 30-second timeout; interpolated variables (e.g. `{{file}}`) are shell-quoted to prevent injection
- Session history stored locally, never sent to the server

### Troubleshooting

```bash
arcanflows health                   # Check server connectivity
arcanflows version                  # Show CLI version and platform
arcanflows config view              # Verify configuration
arcanflows whoami                   # Verify authentication
```

Common issues:
- **"Unauthorized"**: the CLI auto-refreshes an expired access token using your stored refresh token; if it still fails (refresh token expired), run `arcanflows login`
- **"Connection refused"**: Check server URL with `arcanflows health`
- **"Agent not found"**: Run `arcanflows agents list` to see available agents
- **Slow responses**: Check `arcanflows config view` for timeout settings

---

## License

Copyright 2024-2026 ArcanFlows. All rights reserved.
