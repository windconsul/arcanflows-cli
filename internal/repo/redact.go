package repo

import "regexp"

// Secret patterns redacted before any file content is uploaded to the server
// for analysis (PROPOSAL-010 C22). This is best-effort defense-in-depth on top
// of .gitignore — a repo audit shouldn't be the thing that leaks your keys.
var secretPatterns = []*regexp.Regexp{
	// PEM private key blocks.
	regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`),
	// Cloud / provider tokens with distinctive prefixes.
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),                     // AWS access key id
	regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{20,}`),           // GitHub tokens
	regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`),         // Slack tokens
	regexp.MustCompile(`sk-[A-Za-z0-9]{20,}`),                  // OpenAI-style keys
	regexp.MustCompile(`AIza[0-9A-Za-z_\-]{30,}`),              // Google API keys
	// JWTs.
	regexp.MustCompile(`eyJ[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{8,}`),
}

// secretAssignment matches `key = "value"` style secrets, redacting only the
// value so the surrounding code still reads sensibly.
var secretAssignment = regexp.MustCompile(
	`(?i)((?:api[_-]?key|secret|token|password|passwd|access[_-]?key|private[_-]?key|client[_-]?secret)\s*[:=]\s*['"]?)([A-Za-z0-9_\-./+=]{8,})(['"]?)`)

// Redact replaces detected secrets with [REDACTED] and returns the cleaned text
// plus the number of redactions made.
func Redact(text string) (string, int) {
	count := 0
	out := text
	for _, re := range secretPatterns {
		out = re.ReplaceAllStringFunc(out, func(m string) string {
			count++
			return "[REDACTED]"
		})
	}
	out = secretAssignment.ReplaceAllStringFunc(out, func(m string) string {
		sub := secretAssignment.FindStringSubmatch(m)
		if len(sub) != 4 {
			return m
		}
		count++
		return sub[1] + "[REDACTED]" + sub[3]
	})
	return out, count
}
