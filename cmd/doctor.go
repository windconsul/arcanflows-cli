package cmd

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/arcanflows/cli/internal/api"
	"github.com/arcanflows/cli/internal/render"
	"github.com/spf13/cobra"
)

var doctorFix bool

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnose your CLI setup (config, auth, server, version)",
	Long: `Run a health check on your ArcanFlows CLI setup and report what's wrong.

Read-only by default — it only reports and suggests the fix command for each
issue. Pass --fix to apply the safe fixes automatically (refresh an expired
token, install the latest CLI version).

Examples:
  arcanflows doctor          Report the status of every check
  arcanflows doctor --fix    Report, then apply safe fixes`,
	RunE: runDoctor,
}

func init() {
	doctorCmd.Flags().BoolVar(&doctorFix, "fix", false, "apply safe fixes (refresh token, self-update) instead of only reporting")
	rootCmd.AddCommand(doctorCmd)
}

// check statuses
const (
	stOK   = "ok"
	stWarn = "warn"
	stFail = "fail"
)

type check struct {
	status string
	label  string
	detail string
	fix    string // suggested command / action when not ok
}

func runDoctor(cmd *cobra.Command, args []string) error {
	fmt.Fprintf(os.Stderr, "\n%sArcanFlows CLI doctor%s  %s(%s)%s\n\n", render.Bold, render.Reset, render.Dim, Version, render.Reset)

	var checks []check
	server := cfg.ActiveServer()
	active := creds.GetActive(cfg.ActiveProfile)

	// 1. Config + active profile + server
	switch {
	case cfg == nil || len(cfg.Profiles) == 0:
		checks = append(checks, check{stFail, "Config", "no profiles found in ~/.arcanflows/config.yaml", "arcanflows login"})
	case server == "":
		checks = append(checks, check{stFail, "Config", "active profile has no server URL", "arcanflows config set server <url>"})
	default:
		checks = append(checks, check{stOK, "Config", fmt.Sprintf("profile %q → %s", cfg.ActiveProfile, server), ""})
	}

	// 2. Credentials present
	usingAPIKey := active.APIKey != ""
	switch {
	case active.AccessToken == "" && active.APIKey == "":
		checks = append(checks, check{stFail, "Credentials", "not logged in (no token or API key)", "arcanflows login"})
	case usingAPIKey:
		checks = append(checks, check{stOK, "Credentials", "using an API key", ""})
	default:
		who := active.Email
		if who == "" {
			who = "JWT token"
		}
		checks = append(checks, check{stOK, "Credentials", who, ""})
	}

	// 3. Token expiry (JWT only) — decode locally, no network.
	if !usingAPIKey && active.AccessToken != "" {
		exp, ok := jwtExpiry(active.AccessToken)
		switch {
		case !ok:
			checks = append(checks, check{stWarn, "Token", "couldn't read token expiry (unusual token format)", ""})
		case time.Now().After(exp):
			checks = append(checks, check{stFail, "Token", fmt.Sprintf("expired %s ago", roundDur(time.Since(exp))), "arcanflows login (or --fix to refresh)"})
		case time.Until(exp) < 30*time.Minute:
			checks = append(checks, check{stWarn, "Token", fmt.Sprintf("expires in %s", roundDur(time.Until(exp))), "--fix to refresh now"})
		default:
			checks = append(checks, check{stOK, "Token", fmt.Sprintf("valid for %s", roundDur(time.Until(exp))), ""})
		}
	}

	// --fix: refresh an expired/near-expiry token before the auth probe.
	if doctorFix && !usingAPIKey && active.RefreshToken != "" {
		if exp, ok := jwtExpiry(active.AccessToken); ok && time.Until(exp) < 30*time.Minute {
			client := newAPIClient()
			if err := client.Refresh(); err == nil {
				active = creds.GetActive(cfg.ActiveProfile) // reload the refreshed token
				checks = append(checks, check{stOK, "Fix", "refreshed the access token", ""})
			} else {
				checks = append(checks, check{stWarn, "Fix", "token refresh failed — log in again", "arcanflows login"})
			}
		}
	}

	// 4. Server reachable. Any HTTP response means the server is up — the /health
	// path isn't always exposed at the public origin, so a non-200 (e.g. 404) is
	// still "reachable"; only a transport/DNS/timeout error is a real failure.
	if server != "" {
		if code, ms, err := ping(server + "/health"); err != nil {
			checks = append(checks, check{stFail, "Server", fmt.Sprintf("unreachable: %v", err), "check your network or 'arcanflows config set server <url>'"})
		} else if code >= 500 {
			checks = append(checks, check{stWarn, "Server", fmt.Sprintf("reachable but /health returned HTTP %d", code), ""})
		} else {
			checks = append(checks, check{stOK, "Server", fmt.Sprintf("reachable (%dms)", ms), ""})
		}
	}

	// 5. Auth accepted by the server (read-only probe — no auto-refresh)
	if server != "" && (active.AccessToken != "" || active.APIKey != "") {
		code, err := authProbe(server)
		switch {
		case err != nil:
			checks = append(checks, check{stWarn, "Auth", fmt.Sprintf("couldn't verify: %v", err), ""})
		case code == 200:
			checks = append(checks, check{stOK, "Auth", "the server accepts your credentials", ""})
		case code == 401 || code == 403:
			checks = append(checks, check{stFail, "Auth", "the server rejected your credentials (expired or revoked)", "arcanflows login"})
		default:
			checks = append(checks, check{stWarn, "Auth", fmt.Sprintf("unexpected HTTP %d from the auth probe", code), ""})
		}
	}

	// 6. CLI version currency (GitHub releases)
	release, verErr := fetchLatestRelease()
	switch {
	case verErr != nil:
		checks = append(checks, check{stWarn, "Version", fmt.Sprintf("couldn't check: %v", verErr), ""})
	default:
		latest := strings.TrimPrefix(release.TagName, "v")
		current := strings.TrimPrefix(Version, "v")
		if current == "dev" {
			checks = append(checks, check{stWarn, "Version", fmt.Sprintf("running a dev build (latest release: %s)", release.TagName), ""})
		} else if latest == current {
			checks = append(checks, check{stOK, "Version", fmt.Sprintf("%s is the latest", Version), ""})
		} else {
			checks = append(checks, check{stWarn, "Version", fmt.Sprintf("%s available (you have %s)", release.TagName, Version), "arcanflows update (or --fix)"})
		}
	}

	// Render the checklist.
	fails, warns := 0, 0
	for _, c := range checks {
		mark, color := "✓", render.Green
		switch c.status {
		case stWarn:
			mark, color, warns = "!", render.Yellow, warns+1
		case stFail:
			mark, color, fails = "✗", render.Red, fails+1
		}
		fmt.Fprintf(os.Stderr, "  %s%s%s  %s%-12s%s %s\n", color, mark, render.Reset, render.Bold, c.label, render.Reset, c.detail)
		if c.status != stOK && c.fix != "" {
			fmt.Fprintf(os.Stderr, "       %s→ %s%s\n", render.Dim, c.fix, render.Reset)
		}
	}
	fmt.Fprintln(os.Stderr)

	// --fix: self-update if a newer version is available.
	if doctorFix && verErr == nil {
		latest := strings.TrimPrefix(release.TagName, "v")
		current := strings.TrimPrefix(Version, "v")
		if current != "dev" && latest != current {
			render.Info("Applying fix: updating the CLI…")
			if err := runUpdate(cmd, nil); err != nil {
				render.Warning(fmt.Sprintf("update failed: %v", err))
			}
		}
	}

	// Summary.
	switch {
	case fails > 0:
		render.Error(fmt.Sprintf("%d problem(s), %d warning(s). Fix the ✗ items above%s.", fails, warns, fixTail()))
		return nil
	case warns > 0:
		render.Warning(fmt.Sprintf("Looks OK, with %d warning(s)%s.", warns, fixTail()))
	default:
		render.Success("Everything looks healthy.")
	}
	return nil
}

func fixTail() string {
	if doctorFix {
		return ""
	}
	return " (or re-run with --fix)"
}


// jwtExpiry decodes a JWT's `exp` claim without verifying the signature.
func jwtExpiry(token string) (time.Time, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		// Some tokens carry padding.
		payload, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return time.Time{}, false
		}
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Exp == 0 {
		return time.Time{}, false
	}
	return time.Unix(claims.Exp, 0), true
}

func roundDur(d time.Duration) string {
	if d < time.Minute {
		return d.Round(time.Second).String()
	}
	if d < time.Hour {
		return d.Round(time.Minute).String()
	}
	return d.Round(time.Hour).String()
}

func ping(url string) (int, int64, error) {
	httpClient := &http.Client{Timeout: 5 * time.Second}
	start := time.Now()
	resp, err := httpClient.Get(url)
	if err != nil {
		return 0, 0, err
	}
	resp.Body.Close()
	return resp.StatusCode, time.Since(start).Milliseconds(), nil
}

// authProbe hits a lightweight authenticated endpoint with the stored
// credentials and returns the status code WITHOUT auto-refreshing the token
// (so `doctor` stays read-only). It never follows the client's 401-refresh path.
// authProbe hits a lightweight authenticated endpoint and returns the status
// code, reading credentials from the shared globals.
func authProbe(server string) (int, error) {
	pc := creds.GetActive(cfg.ActiveProfile)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(server, "/")+"/api/v1/agents?limit=1", nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", api.UserAgent)
	if pc.APIKey != "" {
		req.Header.Set("X-API-Key", pc.APIKey)
	} else if pc.AccessToken != "" {
		req.Header.Set("Authorization", "Bearer "+pc.AccessToken)
	}
	resp, err := (&http.Client{Timeout: 8 * time.Second}).Do(req)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return resp.StatusCode, nil
}

