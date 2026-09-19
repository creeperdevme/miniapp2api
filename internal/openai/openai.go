// Package openai 定義 OpenAI 相容的請求／回應格式，
// 並負責把 chat 訊息轉換成上游可以接受的單一提示文字。
package openai

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Message 是 chat.completions 的輸入訊息。
type Message struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	Name       string          `json:"name,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
}

// TextContent 取出訊息中的純文字（支援字串與多模態陣列兩種格式）。
func (m Message) TextContent() string {
	if len(m.Content) == 0 {
		return ""
	}

	var text string
	if err := json.Unmarshal(m.Content, &text); err == nil {
		return strings.TrimSpace(text)
	}

	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(m.Content, &parts); err == nil {
		collected := make([]string, 0, len(parts))
		for _, part := range parts {
			kind := strings.ToLower(part.Type)
			if kind != "" && !strings.Contains(kind, "text") {
				continue
			}
			if trimmed := strings.TrimSpace(part.Text); trimmed != "" {
				collected = append(collected, trimmed)
			}
		}
		return strings.Join(collected, "\n")
	}

	return strings.TrimSpace(string(m.Content))
}

// StreamOptions 是 OpenAI 的 stream_options 欄位。
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// ChatRequest 是 POST /v1/chat/completions 的請求內容。
type ChatRequest struct {
	Model         string          `json:"model"`
	Messages      []Message       `json:"messages"`
	Stream        bool            `json:"stream"`
	StreamOptions *StreamOptions  `json:"stream_options,omitempty"`
	User          string          `json:"user,omitempty"`
	Temperature   *float64        `json:"temperature,omitempty"`
	TopP          *float64        `json:"top_p,omitempty"`
	MaxTokens     *int            `json:"max_tokens,omitempty"`
	N             *int            `json:"n,omitempty"`
	Stop          json.RawMessage `json:"stop,omitempty"`

	// Prompt 允許直接指定提示文字（部分客戶端會使用）。
	Prompt string `json:"prompt,omitempty"`
}

// IncludeUsage 回報是否需要在串流最後附上 usage。
func (r ChatRequest) IncludeUsage() bool {
	return r.StreamOptions != nil && r.StreamOptions.IncludeUsage
}

// BuildPrompt 把訊息串接成上游看得懂的單一提示。
func (r ChatRequest) BuildPrompt() string {
	if strings.TrimSpace(r.Prompt) != "" && len(r.Messages) == 0 {
		return strings.TrimSpace(r.Prompt)
	}

	var (
		systemParts []string
		turns       []Message
	)
	for _, message := range r.Messages {
		switch strings.ToLower(message.Role) {
		case "system", "developer":
			if text := message.TextContent(); text != "" {
				systemParts = append(systemParts, text)
			}
		default:
			turns = append(turns, message)
		}
	}

	// 只有單一使用者訊息時直接送出原文，最貼近上游原本的使用方式。
	if len(systemParts) == 0 && len(turns) == 1 && strings.EqualFold(turns[0].Role, "user") {
		return turns[0].TextContent()
	}

	var builder strings.Builder
	if len(systemParts) > 0 {
		builder.WriteString(strings.Join(systemParts, "\n\n"))
		builder.WriteString("\n\n")
	}

	for index, message := range turns {
		text := message.TextContent()
		if text == "" {
			continue
		}
		label := roleLabel(message)
		if index == len(turns)-1 {
			fmt.Fprintf(&builder, "%s: %s", label, text)
			break
		}
		fmt.Fprintf(&builder, "%s: %s\n", label, text)
	}

	return strings.TrimSpace(builder.String())
}

func roleLabel(message Message) string {
	switch strings.ToLower(message.Role) {
	case "assistant":
		return "Assistant"
	case "tool", "function":
		if message.Name != "" {
			return "Tool(" + message.Name + ")"
		}
		return "Tool"
	case "user":
		if message.Name != "" {
			return "User(" + message.Name + ")"
		}
		return "User"
	default:
		if message.Role == "" {
			return "User"
		}
		return strings.ToUpper(message.Role[:1]) + message.Role[1:]
	}
}

// Usage 是 token 使用量。
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ResponseMessage 是回應中的訊息。
type ResponseMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Choice 是回應中的單一選項。
type Choice struct {
	Index        int             `json:"index"`
	Message      ResponseMessage `json:"message"`
	FinishReason string          `json:"finish_reason"`
}

// ChatResponse 是 /v1/chat/completions 的回應。
type ChatResponse struct {
	ID                string   `json:"id"`
	Object            string   `json:"object"`
	Created           int64    `json:"created"`
	Model             string   `json:"model"`
	Choices           []Choice `json:"choices"`
	Usage             Usage    `json:"usage"`
	SystemFingerprint string   `json:"system_fingerprint,omitempty"`
}

// Delta 是串流回應中的增量內容。
type Delta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

// ChunkChoice 是串流回應中的單一選項。
type ChunkChoice struct {
	Index        int     `json:"index"`
	Delta        Delta   `json:"delta"`
	FinishReason *string `json:"finish_reason"`
}

// Chunk 是串流回應的單一事件。
type Chunk struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"`
	Created int64         `json:"created"`
	Model   string        `json:"model"`
	Choices []ChunkChoice `json:"choices"`
	Usage   *Usage        `json:"usage,omitempty"`
}

// ErrorDetail 描述 API 錯誤。
type ErrorDetail struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Param   string `json:"param,omitempty"`
	Code    string `json:"code,omitempty"`
}

// ErrorResponse 是 OpenAI 風格的錯誤回應。
type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

// ModelInfo 是 /v1/models 中的單一模型。
type ModelInfo struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// ModelList 是 /v1/models 的回應。
type ModelList struct {
	Object string      `json:"object"`
	Data   []ModelInfo `json:"data"`
}

// EstimateTokens 粗略估算文字長度對應的 token 數。
//
// 拉丁字母約 4 個字元 1 個 token，中日韓文字約 1 個字 1 個 token。
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	var ascii, wide int
	for _, char := range text {
		if char < utf8.RuneSelf {
			ascii++
			continue
		}
		if unicode.Is(unicode.Han, char) || unicode.Is(unicode.Hangul, char) || unicode.Is(unicode.Hiragana, char) || unicode.Is(unicode.Katakana, char) {
			wide++
			continue
		}
		wide++
	}
	tokens := ascii/4 + wide
	if tokens == 0 {
		tokens = 1
	}
	return tokens
}
