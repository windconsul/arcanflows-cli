package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/arcanflows/cli/internal/render"
	"github.com/spf13/cobra"
)

// cliRepo is the public GitHub repo that publishes CLI releases + binaries. It
// matches install.sh (ARCANFLOWS_CLI_REPO, default windconsul/arcanflows-cli) —
// NOT the monorepo, which is private and has no CLI binaries.
func cliRepo() string {
	if r := os.Getenv("ARCANFLOWS_CLI_REPO"); r != "" {
		return r
	}
	return "windconsul/arcanflows-cli"
}

func releaseAPIURL() string {
	return "https://api.github.com/repos/" + cliRepo() + "/releases/latest"
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update ArcanFlows CLI to the latest version",
	Long: `Check for and install the latest version of ArcanFlows CLI.

Examples:
  arcanflows update          Download and install latest version
  arcanflows update check    Check if an update is available`,
	RunE: runUpdate,
}

var updateCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Check if a newer version is available",
	RunE:  runUpdateCheck,
}

func init() {
	updateCmd.AddCommand(updateCheckCmd)
	rootCmd.AddCommand(updateCmd)
}

type githubRelease struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	HTMLURL string `json:"html_url"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
		Size               int64  `json:"size"`
	} `json:"assets"`
	PublishedAt string `json:"published_at"`
}

func fetchLatestRelease() (*githubRelease, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(releaseAPIURL())
	if err != nil {
		return nil, fmt.Errorf("failed to check for updates: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub API returned %d for %s (rate limited or no releases)", resp.StatusCode, cliRepo())
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("failed to parse release info: %w", err)
	}
	return &release, nil
}

func getBinaryName() string {
	name := fmt.Sprintf("arcanflows-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

func runUpdateCheck(cmd *cobra.Command, args []string) error {
	spinner := render.NewSpinner("Checking for updates...")
	spinner.Start()

	release, err := fetchLatestRelease()
	spinner.Stop()

	if err != nil {
		return err
	}

	latest := strings.TrimPrefix(release.TagName, "v")
	current := strings.TrimPrefix(Version, "v")

	if latest == current || current == "dev" {
		render.Success(fmt.Sprintf("ArcanFlows CLI %s is up to date", Version))
		return nil
	}

	fmt.Fprintf(os.Stderr, "\n%sUpdate available!%s\n", render.Bold, render.Reset)
	fmt.Fprintf(os.Stderr, "  Current: %s%s%s\n", render.Yellow, Version, render.Reset)
	fmt.Fprintf(os.Stderr, "  Latest:  %s%s%s\n", render.Green, release.TagName, render.Reset)
	fmt.Fprintf(os.Stderr, "\n  Run %sarcanflows update%s to install\n\n", render.Bold, render.Reset)

	return nil
}

func runUpdate(cmd *cobra.Command, args []string) error {
	spinner := render.NewSpinner("Checking for updates...")
	spinner.Start()

	release, err := fetchLatestRelease()
	spinner.Stop()

	if err != nil {
		return err
	}

	latest := strings.TrimPrefix(release.TagName, "v")
	current := strings.TrimPrefix(Version, "v")

	if latest == current && current != "dev" {
		render.Success(fmt.Sprintf("ArcanFlows CLI %s is already the latest version", Version))
		return nil
	}

	// Find the right asset
	binaryName := getBinaryName()
	var downloadURL string
	for _, asset := range release.Assets {
		if asset.Name == binaryName {
			downloadURL = asset.BrowserDownloadURL
			break
		}
	}

	if downloadURL == "" {
		render.Warning(fmt.Sprintf("No binary found for %s/%s in release %s", runtime.GOOS, runtime.GOARCH, release.TagName))
		fmt.Fprintf(os.Stderr, "  Available assets:\n")
		for _, asset := range release.Assets {
			fmt.Fprintf(os.Stderr, "    - %s\n", asset.Name)
		}
		fmt.Fprintf(os.Stderr, "\n  Manual download: %s\n\n", release.HTMLURL)
		return nil
	}

	fmt.Fprintf(os.Stderr, "%sUpdating %s -> %s%s\n", render.Dim, Version, release.TagName, render.Reset)

	// Get current binary path
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot determine current binary path: %w", err)
	}

	// Download to temp file
	spinner = render.NewSpinner("Downloading...")
	spinner.Start()

	tmpFile := execPath + ".new"
	dlCmd := exec.Command("curl", "-fsSL", "-o", tmpFile, downloadURL)
	if err := dlCmd.Run(); err != nil {
		spinner.Stop()
		os.Remove(tmpFile)
		return fmt.Errorf("download failed: %w", err)
	}
	spinner.Stop()

	// Make executable
	if err := os.Chmod(tmpFile, 0755); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("chmod failed: %w", err)
	}

	// Replace current binary
	backupPath := execPath + ".bak"
	os.Remove(backupPath) // clean old backup

	if err := os.Rename(execPath, backupPath); err != nil {
		// Try with sudo
		sudoCmd := exec.Command("sudo", "mv", tmpFile, execPath)
		if sudoErr := sudoCmd.Run(); sudoErr != nil {
			os.Remove(tmpFile)
			return fmt.Errorf("failed to replace binary (try: sudo arcanflows update): %w", err)
		}
	} else {
		if err := os.Rename(tmpFile, execPath); err != nil {
			// Restore backup
			os.Rename(backupPath, execPath)
			return fmt.Errorf("failed to install new binary: %w", err)
		}
		os.Remove(backupPath)
	}

	render.Success(fmt.Sprintf("Updated to %s", release.TagName))

	// Show release name
	if release.Name != "" {
		fmt.Fprintf(os.Stderr, "  %s%s%s\n\n", render.Dim, release.Name, render.Reset)
	}

	return nil
}
