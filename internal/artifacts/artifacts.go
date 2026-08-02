// Package artifacts bridges server-generated outputs to local disk
// (PROPOSAL-010 §6.6): when an agent's answer carries platform artifact
// URLs (generated documents, images, media), the CLI offers to save them
// into ./artifacts/ with a manifest — closing the "request things and save
// things locally" loop for non-code outputs.
package artifacts

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// artifactPathPatterns are the platform's artifact-serving routes. URLs are
// only bridged when they point at the CLI's OWN configured server — a link
// to some other host is just a link.
var artifactPathPatterns = []string{
	"/api/v1/public/generated-media/",
	"/api/v1/storage/download/",
	"/api/v1/avatars/",
	"/diagrams/",
}

var urlRe = regexp.MustCompile(`https?://[^\s<>()"']+`)

// FindURLs extracts artifact URLs belonging to serverBase from a response.
func FindURLs(response, serverBase string) []string {
	serverBase = strings.TrimRight(serverBase, "/")
	if serverBase == "" {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, u := range urlRe.FindAllString(response, -1) {
		u = strings.TrimRight(u, ".,;:!?)")
		if !strings.HasPrefix(u, serverBase) && !sameHostArtifact(u, serverBase) {
			continue
		}
		match := false
		for _, p := range artifactPathPatterns {
			if strings.Contains(u, p) {
				match = true
				break
			}
		}
		if match && !seen[u] {
			seen[u] = true
			out = append(out, u)
		}
	}
	return out
}

// sameHostArtifact tolerates the api-subdomain variant of the same server
// (dev6.example.com config vs dev6-api.example.com links).
func sameHostArtifact(u, serverBase string) bool {
	base := strings.TrimPrefix(strings.TrimPrefix(serverBase, "https://"), "http://")
	host := strings.SplitN(strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://"), "/", 2)[0]
	return strings.HasSuffix(host, base) || strings.HasSuffix(base, host) ||
		strings.Contains(host, strings.SplitN(base, ".", 2)[0])
}

type manifestEntry struct {
	Time      time.Time `json:"time"`
	URL       string    `json:"url"`
	File      string    `json:"file"`
	Agent     string    `json:"agent"`
	SizeBytes int       `json:"size_bytes"`
}

// Save downloads each URL via the provided fetch func into ./artifacts/,
// appending a manifest line per file. Collisions get a numeric suffix.
func Save(urls []string, agent string, fetch func(url string) ([]byte, error)) (int, error) {
	if err := os.MkdirAll("artifacts", 0755); err != nil {
		return 0, err
	}
	saved := 0
	for _, u := range urls {
		data, err := fetch(u)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  ✗ %s: %v\n", filepath.Base(u), err)
			continue
		}
		name := filepath.Base(strings.SplitN(u, "?", 2)[0])
		if name == "" || name == "/" {
			name = fmt.Sprintf("artifact-%d", time.Now().UnixNano())
		}
		dest := filepath.Join("artifacts", name)
		for i := 1; ; i++ {
			if _, err := os.Stat(dest); os.IsNotExist(err) {
				break
			}
			ext := filepath.Ext(name)
			dest = filepath.Join("artifacts", fmt.Sprintf("%s-%d%s", strings.TrimSuffix(name, ext), i, ext))
		}
		if err := os.WriteFile(dest, data, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "  ✗ %s: %v\n", name, err)
			continue
		}
		appendManifest(manifestEntry{Time: time.Now(), URL: u, File: dest, Agent: agent, SizeBytes: len(data)})
		fmt.Fprintf(os.Stderr, "  ✓ %s (%d bytes)\n", dest, len(data))
		saved++
	}
	return saved, nil
}

func appendManifest(e manifestEntry) {
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join("artifacts", "manifest.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(data, '\n'))
}
