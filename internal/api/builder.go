package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// BuilderTurn is one prior message in the Builder conversation.
type BuilderTurn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// PlannedRef is a component the Builder announced it will draft (plan event).
type PlannedRef struct {
	Ref  string `json:"ref"`
	Type string `json:"type"`
}

// BuilderChatRequest is the body for POST /builder/chat/stream. Proposals and
// existing components are kept as raw JSON so they round-trip to /execute
// verbatim (the Builder needs the full spec + wiring fields, not just display).
type BuilderChatRequest struct {
	Message            string            `json:"message"`
	History            []BuilderTurn     `json:"history,omitempty"`
	CurrentProposals   []json.RawMessage `json:"current_proposals,omitempty"`
	ConversationID     string            `json:"conversation_id,omitempty"`
	ExistingComponents []json.RawMessage `json:"existing_components,omitempty"`
}

// BuilderEvent is one SSE event from the Builder stream: a "plan" (message +
// planned refs), a "proposal" (a drafted component), a "failure", or "done".
type BuilderEvent struct {
	Type     string          `json:"type"`
	Message  string          `json:"message,omitempty"`
	Planned  []PlannedRef    `json:"planned,omitempty"`
	Proposal json.RawMessage `json:"proposal,omitempty"`
	Ref      string          `json:"ref,omitempty"`
	CompType string          `json:"comp_type,omitempty"`
	Reason   string          `json:"reason,omitempty"`
}

// ProposalMeta extracts the human-facing fields from a raw proposal for display.
type ProposalMeta struct {
	Ref      string `json:"ref"`
	Type     string `json:"type"`
	Name     string `json:"name"`
	Summary  string `json:"summary"`
	UpdateOf string `json:"update_of,omitempty"`
}

// BuilderExecuteRequest is the body for POST /builder/execute.
type BuilderExecuteRequest struct {
	Proposals      []json.RawMessage `json:"proposals"`
	History        []BuilderTurn     `json:"history,omitempty"`
	Title          string            `json:"title,omitempty"`
	ConversationID string            `json:"conversation_id,omitempty"`
}

// CreatedComponent is one component created by /builder/execute.
type CreatedComponent struct {
	Ref  string `json:"ref"`
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

// ComponentHealth is the post-build verification of one created component.
type ComponentHealth struct {
	Ref    string   `json:"ref"`
	Type   string   `json:"type"`
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Status string   `json:"status"` // ok | warning
	Issues []string `json:"issues,omitempty"`
}

// BuildResult is the response from /builder/execute.
type BuildResult struct {
	Created []CreatedComponent `json:"created"`
	Summary string             `json:"summary,omitempty"`
	Health  []ComponentHealth  `json:"health,omitempty"`
	Error   string             `json:"error,omitempty"`
	Partial bool               `json:"partial,omitempty"`
}

// BuilderEventCallback receives Builder SSE events.
type BuilderEventCallback func(ev BuilderEvent)

// BuilderChatStream runs one Builder planning turn and streams its events
// (plan → proposals/failures → done) via SSE. Bound to ctx so Ctrl+C aborts.
func (c *Client) BuilderChatStream(ctx context.Context, req BuilderChatRequest, cb BuilderEventCallback) error {
	resp, err := c.StreamPostContext(ctx, "/api/v1/builder/chat/stream", req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var errBody map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&errBody)
		if msg, ok := errBody["error"].(string); ok {
			return fmt.Errorf("builder error: %s", msg)
		}
		return fmt.Errorf("builder error: HTTP %d", resp.StatusCode)
	}

	// bufio.Reader (not Scanner) so a large proposal spec streamed as one frame
	// doesn't hit the scanner's line-length limit.
	reader := bufio.NewReader(resp.Body)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line, readErr := reader.ReadString('\n')
		if line != "" {
			line = strings.TrimRight(line, "\r\n")
			if strings.HasPrefix(line, "data: ") {
				data := strings.TrimPrefix(line, "data: ")
				if data != "" && data != "[DONE]" {
					var ev BuilderEvent
					if err := json.Unmarshal([]byte(data), &ev); err == nil {
						cb(ev)
					}
				}
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				return nil
			}
			return fmt.Errorf("stream read error: %w", readErr)
		}
	}
}

// BuilderExecute creates the approved proposals (the "Build" step) and returns
// the created components + a post-build health check.
func (c *Client) BuilderExecute(req BuilderExecuteRequest) (*BuildResult, error) {
	var result BuildResult
	if err := c.Post("/api/v1/builder/execute", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
