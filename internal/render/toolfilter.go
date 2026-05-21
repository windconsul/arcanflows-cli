package render

import (
	"fmt"
	"os"
	"strings"
)

// ToolFilter detects and suppresses USE_TOOL: directives from streamed LLM output.
type ToolFilter struct {
	buffer     strings.Builder
	toolsShown map[string]bool
	suppressing bool   // true while eating a tool call block
	Output     strings.Builder
}

// NewToolFilter creates a new filter.
func NewToolFilter() *ToolFilter {
	return &ToolFilter{
		toolsShown: make(map[string]bool),
	}
}

// ProcessDelta processes a streaming delta token. Returns text to print.
func (f *ToolFilter) ProcessDelta(delta string) string {
	f.buffer.WriteString(delta)
	buffered := f.buffer.String()

	// If suppressing (inside tool call block), check if we should stop
	if f.suppressing {
		// The tool call block is: USE_TOOL: slug\nINPUT: {...}\n
		// Once the backend executes the tool and restarts the stream,
		// we get fresh content. Detect end by seeing content that's
		// clearly NOT part of a tool call.
		trimmed := strings.TrimSpace(buffered)

		// Still part of tool call — suppress
		if trimmed == "" ||
			strings.HasPrefix(trimmed, "INPUT:") ||
			strings.HasPrefix(trimmed, "{") ||
			strings.HasPrefix(trimmed, "}") ||
			strings.HasPrefix(trimmed, "\"") ||
			strings.HasPrefix(trimmed, "USE_TOOL:") {
			f.buffer.Reset()
			return ""
		}

		// Content resumed — stop suppressing
		f.suppressing = false
		f.buffer.Reset()
		f.Output.WriteString(buffered)
		return buffered
	}

	// Detect and suppress diagram rendering errors from backend
	if strings.Contains(buffered, "Diagram rendering failed:") ||
		strings.Contains(buffered, "No PNG file generated") {
		// Eat the entire error block
		f.suppressing = true
		f.buffer.Reset()
		return ""
	}

	// Detect USE_TOOL: in buffer
	if strings.Contains(buffered, "USE_TOOL:") {
		f.suppressing = true

		// Extract and print content BEFORE the tool call
		idx := strings.Index(buffered, "USE_TOOL:")
		before := strings.TrimRight(buffered[:idx], " \n\t")

		// Show tool name
		after := buffered[idx+9:]
		toolSlug := strings.TrimSpace(strings.Split(after, "\n")[0])
		if toolSlug != "" && !f.toolsShown[toolSlug] {
			fmt.Fprintf(os.Stderr, "\n%s  [Using tool: %s...]%s", Dim, toolSlug, Reset)
			f.toolsShown[toolSlug] = true
		}

		f.buffer.Reset()
		if before != "" {
			f.Output.WriteString(before)
			return before
		}
		return ""
	}

	// Check for partial "USE_TOOL:" building up
	trimmed := strings.TrimLeft(buffered, " \n\t")
	if len(trimmed) <= 10 {
		for _, prefix := range []string{"U", "US", "USE", "USE_", "USE_T", "USE_TO", "USE_TOO", "USE_TOOL", "USE_TOOL:"} {
			if trimmed == prefix {
				return "" // keep buffering
			}
		}
	}

	// Safe to output
	f.buffer.Reset()
	f.Output.WriteString(buffered)
	return buffered
}

// ProcessFinalContent cleans tool directives from final content.
func (f *ToolFilter) ProcessFinalContent(content string) string {
	result := content
	for {
		idx := strings.Index(result, "USE_TOOL:")
		if idx < 0 {
			break
		}
		end := strings.Index(result[idx:], "\n\n")
		if end < 0 {
			result = strings.TrimSpace(result[:idx])
		} else {
			result = result[:idx] + result[idx+end+2:]
		}
	}
	return strings.TrimSpace(result)
}

// Flush returns any remaining buffered content.
func (f *ToolFilter) Flush() string {
	if f.buffer.Len() == 0 {
		return ""
	}
	content := f.buffer.String()
	f.buffer.Reset()
	if f.suppressing {
		return ""
	}
	f.Output.WriteString(content)
	return content
}
