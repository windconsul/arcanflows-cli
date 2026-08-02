// Package rules implements layered user rules (PROPOSAL-010 §6.1).
//
// Two layers merge, global first:
//
//	~/.arcanflows/RULES.md   — the user's rules, every project
//	./ARCANFLOWS.md          — the project's rules (loaded elsewhere; this
//	                           package only parses its [deny] section)
//
// Everything outside a [deny] section is prompt guidance — it steers the
// model. A [deny] section is different: the CLI ENFORCES it mechanically at
// apply time, alongside the protected-path list. Prompts steer; the CLI
// enforces. Syntax inside [deny] (until the next [section] or EOF):
//
//	path: <glob>        e.g.  path: deploy/**        path: *.pem
//	command: <substr>   e.g.  command: git push --force
package rules

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Rules struct {
	// GlobalText is the prose portion of ~/.arcanflows/RULES.md (deny
	// section stripped) — prepended to the project instructions.
	GlobalText string
	// DenyPaths / DenyCommands merge BOTH layers' [deny] sections.
	DenyPaths    []string
	DenyCommands []string
	// Sources lists the files that contributed, for `arcanflows rules`.
	Sources []string
	// Hash fingerprints the effective rule set (drift visibility).
	Hash string
}

// Load reads both layers. A missing file is not an error — rules are optional.
func Load(projectRoot string) *Rules {
	r := &Rules{}
	var hashInput strings.Builder

	if home, err := os.UserHomeDir(); err == nil {
		globalPath := filepath.Join(home, ".arcanflows", "RULES.md")
		if data, err := os.ReadFile(globalPath); err == nil {
			prose, denyP, denyC := splitDeny(string(data))
			r.GlobalText = strings.TrimSpace(prose)
			r.DenyPaths = append(r.DenyPaths, denyP...)
			r.DenyCommands = append(r.DenyCommands, denyC...)
			r.Sources = append(r.Sources, globalPath)
			hashInput.WriteString(string(data))
		}
	}

	projPath := filepath.Join(projectRoot, "ARCANFLOWS.md")
	if data, err := os.ReadFile(projPath); err == nil {
		// The prose half of ARCANFLOWS.md is loaded by the chat command as
		// project instructions; here we only harvest its [deny] section.
		_, denyP, denyC := splitDeny(string(data))
		r.DenyPaths = append(r.DenyPaths, denyP...)
		r.DenyCommands = append(r.DenyCommands, denyC...)
		r.Sources = append(r.Sources, projPath)
		hashInput.WriteString(string(data))
	}

	sum := sha256.Sum256([]byte(hashInput.String()))
	r.Hash = hex.EncodeToString(sum[:8])
	return r
}

// splitDeny separates prose from [deny] entries.
func splitDeny(content string) (prose string, denyPaths, denyCommands []string) {
	var proseB strings.Builder
	inDeny := false
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.EqualFold(trimmed, "[deny]"):
			inDeny = true
			continue
		case strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]"):
			inDeny = false
		}
		if !inDeny {
			proseB.WriteString(line)
			proseB.WriteString("\n")
			continue
		}
		if v, ok := strings.CutPrefix(trimmed, "path:"); ok {
			if p := strings.TrimSpace(v); p != "" {
				denyPaths = append(denyPaths, p)
			}
		} else if v, ok := strings.CutPrefix(trimmed, "command:"); ok {
			if c := strings.TrimSpace(v); c != "" {
				denyCommands = append(denyCommands, c)
			}
		}
	}
	return proseB.String(), denyPaths, denyCommands
}

// CheckPath returns an error when the path matches a deny glob. Globs match
// against the full relative path AND the basename ("*.pem" catches
// certs/server.pem; "deploy/**" catches everything under deploy/).
func (r *Rules) CheckPath(path string) error {
	if r == nil {
		return nil
	}
	clean := filepath.ToSlash(filepath.Clean(path))
	base := filepath.Base(clean)
	for _, g := range r.DenyPaths {
		g = filepath.ToSlash(g)
		if ok, _ := filepath.Match(g, clean); ok {
			return fmt.Errorf("blocked by [deny] rule %q", g)
		}
		if ok, _ := filepath.Match(g, base); ok {
			return fmt.Errorf("blocked by [deny] rule %q", g)
		}
		// dir/** semantics: prefix match on the directory.
		if strings.HasSuffix(g, "/**") {
			if strings.HasPrefix(clean, strings.TrimSuffix(g, "/**")+"/") {
				return fmt.Errorf("blocked by [deny] rule %q", g)
			}
		}
	}
	return nil
}

// CheckCommand returns an error when the command contains a denied fragment.
func (r *Rules) CheckCommand(cmd string) error {
	if r == nil {
		return nil
	}
	for _, frag := range r.DenyCommands {
		if strings.Contains(cmd, frag) {
			return fmt.Errorf("blocked by [deny] rule %q", frag)
		}
	}
	return nil
}
