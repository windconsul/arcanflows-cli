package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// ChatRequest is the request body for streaming chat.
type ChatRequest struct {
	Message        string `json:"message"`
	ConversationID string `json:"conversation_id,omitempty"`
}

// StreamChunk represents a single SSE chunk from the chat stream.
type StreamChunk struct {
	Delta   string `json:"delta,omitempty"`
	Content string `json:"content,omitempty"`
	Done    bool   `json:"done,omitempty"`

	// Metadata event fields
	Type           string `json:"type,omitempty"`
	ConversationID string `json:"conversation_id,omitempty"`
	Model          string `json:"model,omitempty"`

	// Thinking
	Thinking string `json:"thinking,omitempty"`

	// Tool usage
	ToolStatus string `json:"tool_status,omitempty"`

	// Token counts
	PromptTokens     int `json:"prompt_tokens,omitempty"`
	CompletionTokens int `json:"completion_tokens,omitempty"`
	TotalTokens      int `json:"total_tokens,omitempty"`
}

// ChatStreamCallback receives streaming chunks.
type ChatStreamCallback func(chunk StreamChunk)

// ChatStream sends a message and streams the response via SSE.
func (c *Client) ChatStream(agentID string, req ChatRequest, callback ChatStreamCallback) error {
	return c.ChatStreamContext(context.Background(), agentID, req, callback)
}

// ChatStreamContext is ChatStream bound to a cancellable context. When ctx is
// cancelled (e.g. Ctrl+C on the current turn), the underlying HTTP request is
// aborted and this returns ctx.Err() (PROPOSAL-010 1.3).
func (c *Client) ChatStreamContext(ctx context.Context, agentID string, req ChatRequest, callback ChatStreamCallback) error {
	resp, err := c.StreamPostContext(ctx, fmt.Sprintf("/api/v1/agents/%s/chat/stream", agentID), req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var errBody map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&errBody)
		if msg, ok := errBody["error"].(string); ok {
			return fmt.Errorf("chat error: %s", msg)
		}
		return fmt.Errorf("chat error: HTTP %d", resp.StatusCode)
	}

	// Use a bufio.Reader (not Scanner) so an SSE line larger than 1 MiB — a big
	// tool result streamed as one data frame — doesn't error the stream with
	// "token too long" (PROPOSAL-010 C20). ReadString grows its buffer as needed.
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
				if data == "[DONE]" {
					callback(StreamChunk{Done: true})
					return nil
				}
				var chunk StreamChunk
				if err := json.Unmarshal([]byte(data), &chunk); err == nil {
					callback(chunk)
					if chunk.Done {
						return nil
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
