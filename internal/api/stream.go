package api

import (
	"bufio"
	"encoding/json"
	"fmt"
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
	resp, err := c.StreamPost(fmt.Sprintf("/api/v1/agents/%s/chat/stream", agentID), req)
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

	scanner := bufio.NewScanner(resp.Body)
	// Increase buffer for large responses
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()

		// SSE format: "data: {json}"
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			callback(StreamChunk{Done: true})
			return nil
		}

		var chunk StreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			// Skip unparseable chunks
			continue
		}

		callback(chunk)

		if chunk.Done {
			return nil
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("stream read error: %w", err)
	}

	return nil
}
