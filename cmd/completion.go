package cmd

import (
	"fmt"
	"os"

	"github.com/arcanflows/cli/internal/render"
	"github.com/spf13/cobra"
)

var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Generate shell completion scripts",
	Long: `Generate shell completion scripts for ArcanFlows CLI.

Add the completion to your shell profile for persistent tab-completion.

Bash:
  # Linux
  arcanflows completion bash | sudo tee /etc/bash_completion.d/arcanflows > /dev/null

  # macOS (requires bash-completion)
  arcanflows completion bash > $(brew --prefix)/etc/bash_completion.d/arcanflows

Zsh:
  # Add to your ~/.zshrc:
  arcanflows completion zsh > "${fpath[1]}/_arcanflows"

  # Or use oh-my-zsh:
  arcanflows completion zsh > ~/.oh-my-zsh/completions/_arcanflows

Fish:
  arcanflows completion fish > ~/.config/fish/completions/arcanflows.fish

PowerShell:
  arcanflows completion powershell | Out-String | Invoke-Expression`,
	Args:      cobra.ExactArgs(1),
	ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
	RunE:      runCompletion,
}

func init() {
	rootCmd.AddCommand(completionCmd)
}

func runCompletion(cmd *cobra.Command, args []string) error {
	switch args[0] {
	case "bash":
		if err := rootCmd.GenBashCompletion(os.Stdout); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "\n")
		render.Success("Bash completion generated. See 'arcanflows completion --help' for install instructions.")
	case "zsh":
		if err := rootCmd.GenZshCompletion(os.Stdout); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "\n")
		render.Success("Zsh completion generated. See 'arcanflows completion --help' for install instructions.")
	case "fish":
		if err := rootCmd.GenFishCompletion(os.Stdout, true); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "\n")
		render.Success("Fish completion generated. See 'arcanflows completion --help' for install instructions.")
	case "powershell":
		if err := rootCmd.GenPowerShellCompletionWithDesc(os.Stdout); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "\n")
		render.Success("PowerShell completion generated. See 'arcanflows completion --help' for install instructions.")
	default:
		return fmt.Errorf("unsupported shell: %s (use bash, zsh, fish, or powershell)", args[0])
	}
	return nil
}
