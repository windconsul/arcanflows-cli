package cmd

import (
	"fmt"
	"net/http"
	"time"

	"github.com/arcanflows/cli/internal/render"
	"github.com/spf13/cobra"
)

var healthCmd = &cobra.Command{
	Use:   "health",
	Short: "Check ArcanFlows server health",
	RunE:  runHealth,
}

func init() {
	rootCmd.AddCommand(healthCmd)
}

func runHealth(cmd *cobra.Command, args []string) error {
	server := cfg.ActiveServer()
	fmt.Printf("\n  %sServer:%s %s\n\n", render.Dim, render.Reset, server)

	endpoints := []struct {
		name string
		path string
	}{
		{"API", "/health"},
	}

	allHealthy := true
	httpClient := &http.Client{Timeout: 5 * time.Second}

	for _, ep := range endpoints {
		start := time.Now()
		resp, err := httpClient.Get(server + ep.path)
		elapsed := time.Since(start)

		if err != nil {
			fmt.Printf("  %s%s %s%s (%v)\n", render.Red, "x", ep.name, render.Reset, err)
			allHealthy = false
			continue
		}
		resp.Body.Close()

		if resp.StatusCode == 200 {
			fmt.Printf("  %s%s %s%s (%dms)\n", render.Green, "v", ep.name, render.Reset, elapsed.Milliseconds())
		} else {
			fmt.Printf("  %s%s %s%s (HTTP %d, %dms)\n", render.Red, "x", ep.name, render.Reset, resp.StatusCode, elapsed.Milliseconds())
			allHealthy = false
		}
	}

	fmt.Println()
	if allHealthy {
		render.Success("All services healthy")
	} else {
		render.Warning("Some services are unhealthy")
	}
	return nil
}
