package cmd

import (
	"fmt"
	"os"
	"strconv"

	"github.com/arcanflows/cli/internal/render"
	"github.com/arcanflows/cli/internal/session"
	"github.com/spf13/cobra"
)

var sessionsCmd = &cobra.Command{
	Use:     "sessions",
	Aliases: []string{"history"},
	Short:   "Manage chat session history",
	Long: `List, resume, and manage saved chat sessions.
Sessions are automatically saved during chat and can be resumed later.

Examples:
  arcanflows sessions list
  arcanflows sessions resume 1
  arcanflows sessions delete 20260512-143022
  arcanflows sessions clear`,
}

var sessListCmd = &cobra.Command{
	Use:   "list",
	Short: "List recent chat sessions",
	RunE:  runSessList,
}

var sessResumeCmd = &cobra.Command{
	Use:   "resume [number or session-id]",
	Short: "Resume a saved chat session",
	Args:  cobra.ExactArgs(1),
	RunE:  runSessResume,
}

var sessDeleteCmd = &cobra.Command{
	Use:   "delete [number or session-id]",
	Short: "Delete a saved session",
	Args:  cobra.ExactArgs(1),
	RunE:  runSessDelete,
}

var sessClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Clear all saved sessions",
	RunE:  runSessClear,
}

var sessShowCmd = &cobra.Command{
	Use:   "show [number or session-id]",
	Short: "Show session details and messages",
	Args:  cobra.ExactArgs(1),
	RunE:  runSessShow,
}

func init() {
	sessionsCmd.AddCommand(sessListCmd)
	sessionsCmd.AddCommand(sessResumeCmd)
	sessionsCmd.AddCommand(sessDeleteCmd)
	sessionsCmd.AddCommand(sessClearCmd)
	sessionsCmd.AddCommand(sessShowCmd)

	sessListCmd.Flags().IntP("limit", "n", 10, "number of sessions to show")

	rootCmd.AddCommand(sessionsCmd)
}

func getSessionStore() *session.Store {
	maxAge := cfg.Settings.SessionMaxAgeDays
	if maxAge <= 0 {
		maxAge = 30
	}
	return session.NewStore(maxAge)
}

// resolveSessionID resolves a numeric index (1-based) or direct session ID.
func resolveSessionID(store *session.Store, arg string) (string, error) {
	// Try as numeric index first
	if num, err := strconv.Atoi(arg); err == nil && num >= 1 {
		sessions, err := store.List()
		if err != nil {
			return "", err
		}
		if num > len(sessions) {
			return "", fmt.Errorf("session #%d not found (only %d sessions)", num, len(sessions))
		}
		return sessions[num-1].ID, nil
	}

	// Use as direct session ID
	return arg, nil
}

func runSessList(cmd *cobra.Command, args []string) error {
	store := getSessionStore()
	limit, _ := cmd.Flags().GetInt("limit")

	sessions, err := store.List()
	if err != nil {
		return err
	}

	if len(sessions) == 0 {
		render.Info("No saved sessions. Start a chat with: arcanflows chat")
		return nil
	}

	// Cleanup old sessions in the background
	if cleaned, _ := store.Cleanup(); cleaned > 0 {
		render.Info(fmt.Sprintf("Cleaned up %d expired sessions", cleaned))
	}

	if limit > 0 && len(sessions) > limit {
		sessions = sessions[:limit]
	}

	fmt.Fprintf(os.Stderr, "\n%sRecent Sessions:%s\n\n", render.Bold, render.Reset)

	for i, sess := range sessions {
		timeAgo := session.TimeAgo(sess.LastActivityAt)
		summary := sess.Summary
		if summary == "" {
			summary = "(no messages)"
		}

		fmt.Fprintf(os.Stderr, "  %s%d.%s [%s] %s %s(%d messages, %s)%s\n",
			render.Cyan, i+1, render.Reset,
			timeAgo,
			summary,
			render.Dim, sess.MessageCount, sess.AgentName, render.Reset,
		)

		// Show files modified if any
		if len(sess.FilesModified) > 0 {
			fileCount := len(sess.FilesModified)
			if fileCount <= 3 {
				fmt.Fprintf(os.Stderr, "     %sFiles: %s%s\n",
					render.Dim, joinMax(sess.FilesModified, 3), render.Reset)
			} else {
				fmt.Fprintf(os.Stderr, "     %sFiles: %s (+%d more)%s\n",
					render.Dim, joinMax(sess.FilesModified, 3), fileCount-3, render.Reset)
			}
		}
	}

	fmt.Fprintf(os.Stderr, "\n  %sResume with: arcanflows sessions resume <number>%s\n\n",
		render.Dim, render.Reset)
	return nil
}

func runSessResume(cmd *cobra.Command, args []string) error {
	store := getSessionStore()

	sessionID, err := resolveSessionID(store, args[0])
	if err != nil {
		return err
	}

	sess, err := store.Load(sessionID)
	if err != nil {
		return err
	}

	// Resume by launching chat with the saved conversation ID
	render.Info(fmt.Sprintf("Resuming session with %s (%d messages)", sess.AgentName, sess.MessageCount))

	// Set flags and run chat
	chatCmd.Flags().Set("agent-id", sess.AgentID)
	chatCmd.Flags().Set("conversation-id", sess.ConversationID)
	return runChat(chatCmd, nil)
}

func runSessDelete(cmd *cobra.Command, args []string) error {
	store := getSessionStore()

	sessionID, err := resolveSessionID(store, args[0])
	if err != nil {
		return err
	}

	if err := store.Delete(sessionID); err != nil {
		return err
	}

	render.Success(fmt.Sprintf("Session %s deleted", sessionID))
	return nil
}

func runSessClear(cmd *cobra.Command, args []string) error {
	store := getSessionStore()

	count, err := store.Clear()
	if err != nil {
		return err
	}

	render.Success(fmt.Sprintf("Cleared %d sessions", count))
	return nil
}

func runSessShow(cmd *cobra.Command, args []string) error {
	store := getSessionStore()

	sessionID, err := resolveSessionID(store, args[0])
	if err != nil {
		return err
	}

	sess, err := store.Load(sessionID)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "\n%sSession: %s%s\n", render.Bold, sess.ID, render.Reset)
	fmt.Fprintf(os.Stderr, "  Agent:    %s\n", sess.AgentName)
	fmt.Fprintf(os.Stderr, "  Started:  %s\n", sess.StartedAt.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(os.Stderr, "  Last:     %s (%s)\n", sess.LastActivityAt.Format("2006-01-02 15:04:05"), session.TimeAgo(sess.LastActivityAt))
	fmt.Fprintf(os.Stderr, "  Messages: %d\n", sess.MessageCount)

	if len(sess.FilesModified) > 0 {
		fmt.Fprintf(os.Stderr, "  Files modified:\n")
		for _, f := range sess.FilesModified {
			fmt.Fprintf(os.Stderr, "    - %s\n", f)
		}
	}

	if len(sess.CommandsRun) > 0 {
		fmt.Fprintf(os.Stderr, "  Commands run:\n")
		for _, c := range sess.CommandsRun {
			fmt.Fprintf(os.Stderr, "    - %s\n", c)
		}
	}

	if len(sess.Messages) > 0 {
		fmt.Fprintf(os.Stderr, "\n%sConversation:%s\n\n", render.Bold, render.Reset)
		for _, msg := range sess.Messages {
			ts := msg.Timestamp.Format("15:04")
			if msg.Role == "user" {
				fmt.Fprintf(os.Stderr, "  %s[%s] You:%s %s\n", render.Cyan, ts, render.Reset, msg.Content)
			} else {
				fmt.Fprintf(os.Stderr, "  %s[%s] %s:%s %s\n", render.Green, ts, sess.AgentName, render.Reset, msg.Content)
			}
		}
	}

	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "  %sResume with: arcanflows sessions resume %s%s\n\n",
		render.Dim, sess.ID, render.Reset)

	return nil
}

// joinMax joins up to n strings with ", ".
func joinMax(items []string, n int) string {
	if len(items) <= n {
		return joinStrings(items)
	}
	return joinStrings(items[:n])
}

func joinStrings(items []string) string {
	result := ""
	for i, item := range items {
		if i > 0 {
			result += ", "
		}
		result += item
	}
	return result
}
