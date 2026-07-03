package agent

import (
	"context"
	"encoding/json"
)

const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// Message is the OpenAI-compatible conversation representation used by the
// agent loop. Tool calls are emitted by assistant messages; tool results are
// appended as role=tool messages correlated by ToolCallID.
type Message struct {
	Role             string     `json:"role"`
	Content          string     `json:"content"`
	ReasoningContent string     `json:"-"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
	Name             string     `json:"name,omitempty"`
}

func (m *Message) UnmarshalJSON(data []byte) error {
	var decoded struct {
		Role              string          `json:"role"`
		Content           *string         `json:"content"`
		ReasoningContent  *string         `json:"reasoning_content"`
		ReasoningContent2 *string         `json:"reasoningContent"`
		ReasoningText     *string         `json:"reasoning_text"`
		Reasoning         json.RawMessage `json:"reasoning"`
		ToolCalls         []ToolCall      `json:"tool_calls"`
		ToolCallID        string          `json:"tool_call_id"`
		Name              string          `json:"name"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	m.Role = decoded.Role
	if decoded.Content != nil {
		m.Content = *decoded.Content
	} else {
		m.Content = ""
	}
	m.ReasoningContent = firstNonEmptyString(decoded.ReasoningContent, decoded.ReasoningContent2, decoded.ReasoningText)
	if m.ReasoningContent == "" {
		m.ReasoningContent = reasoningContentFromRaw(decoded.Reasoning)
	}
	m.ToolCalls = decoded.ToolCalls
	m.ToolCallID = decoded.ToolCallID
	m.Name = decoded.Name
	return nil
}

func firstNonEmptyString(values ...*string) string {
	for _, value := range values {
		if value != nil && *value != "" {
			return *value
		}
	}
	return ""
}

func reasoningContentFromRaw(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var object struct {
		Content string `json:"content"`
		Text    string `json:"text"`
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal(raw, &object); err != nil {
		return ""
	}
	return firstNonEmptyValue(object.Content, object.Text, object.Summary)
}

func firstNonEmptyValue(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

type ToolCall struct {
	ID       string       `json:"id"`
	Index    *int         `json:"index,omitempty"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolDefinition is the model-facing form of an MCP tool.
type ToolDefinition struct {
	Type     string       `json:"type"`
	Function FunctionTool `json:"function"`
}

type FunctionTool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters"`
}

type Completion struct {
	Message      Message
	FinishReason string
	Usage        TokenUsage
}

type TokenUsage struct {
	PromptTokens     int
	CompletionTokens int
	ReasoningTokens  int
	TotalTokens      int
}

// ToolResult is a normalized MCP result suitable for a role=tool message.
type ToolResult struct {
	Content string
	IsError bool
}

type ModelClient interface {
	Complete(ctx context.Context, messages []Message, tools []ToolDefinition) (Completion, error)
}

type ReasoningDeltaObserver func(delta string)

type reasoningDeltaObserverKey struct{}

func WithReasoningDeltaObserver(ctx context.Context, observer ReasoningDeltaObserver) context.Context {
	if observer == nil {
		return ctx
	}
	return context.WithValue(ctx, reasoningDeltaObserverKey{}, observer)
}

func ReasoningDeltaObserverFromContext(ctx context.Context) ReasoningDeltaObserver {
	observer, _ := ctx.Value(reasoningDeltaObserverKey{}).(ReasoningDeltaObserver)
	return observer
}

// ToolClient is implemented by the MCP adapter. Keeping this interface at the
// service boundary lets the loop be tested without a live MCP server.
type ToolClient interface {
	ListTools(ctx context.Context) ([]ToolDefinition, error)
	CallTool(ctx context.Context, name string, arguments json.RawMessage) (ToolResult, error)
}
