package render

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"
)

// inputReader is the single, shared reader for all interactive stdin input.
// Everything that reads a line from the user — the chat prompt, y/n
// confirmations, pickers — MUST go through this reader. Mixing a bufio.Reader
// (chat loop) with fmt.Scanln (confirmations) on the same fd corrupts buffered
// input; a single shared reader avoids that (PROPOSAL-010 C11).
var inputReader *bufio.Reader

// SetInputReader installs the shared stdin reader (called once by the chat loop).
func SetInputReader(r *bufio.Reader) { inputReader = r }

// ReadLine reads one trimmed line from the shared stdin reader, lazily
// initializing it from os.Stdin if the caller never set one. The bool is false
// on EOF/error.
func ReadLine() (string, bool) {
	if inputReader == nil {
		inputReader = bufio.NewReader(os.Stdin)
	}
	line, err := inputReader.ReadString('\n')
	if err != nil && line == "" {
		return "", false
	}
	return strings.TrimSpace(line), true
}

// Confirm prints a y/n prompt and reads the answer from the shared reader.
// Anything other than y/yes (including empty/EOF) is a safe "no".
func Confirm(prompt string) bool {
	fmt.Fprintf(os.Stderr, "%s%s [y/n]:%s ", Bold, prompt, Reset)
	resp, ok := ReadLine()
	if !ok {
		return false
	}
	resp = strings.ToLower(resp)
	return resp == "y" || resp == "yes"
}

// Colors for terminal output.
const (
	Reset   = "\033[0m"
	Bold    = "\033[1m"
	Dim     = "\033[2m"
	Red     = "\033[31m"
	Green   = "\033[32m"
	Yellow  = "\033[33m"
	Blue    = "\033[34m"
	Magenta = "\033[35m"
	Cyan    = "\033[36m"
	White   = "\033[37m"
)

// Success prints a green success message.
func Success(msg string) {
	fmt.Fprintf(os.Stderr, "%s✓ %s%s\n", Green, msg, Reset)
}

// Error prints a red error message.
func Error(msg string) {
	fmt.Fprintf(os.Stderr, "%s✗ %s%s\n", Red, msg, Reset)
}

// Warning prints a yellow warning message.
func Warning(msg string) {
	fmt.Fprintf(os.Stderr, "%s! %s%s\n", Yellow, msg, Reset)
}

// Info prints a blue info message.
func Info(msg string) {
	fmt.Fprintf(os.Stderr, "%si %s%s\n", Blue, msg, Reset)
}

// Header prints a styled header box.
func Header(title, subtitle string) {
	w := 52
	border := strings.Repeat("-", w-2)
	fmt.Fprintf(os.Stderr, "%s+%s+%s\n", Cyan, border, Reset)
	fmt.Fprintf(os.Stderr, "%s|%s %-*s%s|%s\n", Cyan, Bold+White, w-4, title, Cyan, Reset)
	if subtitle != "" {
		fmt.Fprintf(os.Stderr, "%s|%s %-*s%s|%s\n", Cyan, Dim, w-4, subtitle, Cyan, Reset)
	}
	fmt.Fprintf(os.Stderr, "%s+%s+%s\n", Cyan, border, Reset)
}

// Prompt prints a user input prompt.
func Prompt(label string) {
	fmt.Fprintf(os.Stderr, "%s%s%s > ", Bold+Green, label, Reset)
}

// AgentLabel prints the agent response label.
func AgentLabel(name string) {
	fmt.Fprintf(os.Stderr, "\n%s%s%s > ", Bold+Cyan, name, Reset)
}

// Table prints a formatted table.
func Table(headers []string, rows [][]string) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for i, h := range headers {
		if i > 0 {
			fmt.Fprint(w, "\t")
		}
		fmt.Fprintf(w, "%s%s%s", Bold+Dim, strings.ToUpper(h), Reset)
	}
	fmt.Fprintln(w)
	for _, row := range rows {
		for i, cell := range row {
			if i > 0 {
				fmt.Fprint(w, "\t")
			}
			fmt.Fprint(w, cell)
		}
		fmt.Fprintln(w)
	}
	w.Flush()
}

// Spinner is a simple loading indicator.
type Spinner struct {
	msg  string
	done chan struct{}
}

// NewSpinner creates a spinner with a message.
func NewSpinner(msg string) *Spinner {
	return &Spinner{msg: msg, done: make(chan struct{})}
}

// Start begins the spinner.
func (s *Spinner) Start() {
	frames := []string{"|", "/", "-", "\\"}
	go func() {
		i := 0
		for {
			select {
			case <-s.done:
				fmt.Fprintf(os.Stderr, "\r%s\r", strings.Repeat(" ", len(s.msg)+4))
				return
			default:
				fmt.Fprintf(os.Stderr, "\r%s%s%s %s", Cyan, frames[i%len(frames)], Reset, s.msg)
				i++
				time.Sleep(100 * time.Millisecond)
			}
		}
	}()
}

// Stop ends the spinner.
func (s *Spinner) Stop() {
	select {
	case <-s.done:
	default:
		close(s.done)
	}
	time.Sleep(50 * time.Millisecond) // let goroutine clear the line
}
