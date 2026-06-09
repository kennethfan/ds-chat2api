package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// --- Anthropic Messages API 请求/响应类型 ---

// AnthropicMessageRequest 对应 POST /v1/messages 的请求体
type AnthropicMessageRequest struct {
	Model       string           `json:"model"`
	Messages    []AnthropicMsg   `json:"messages"`
	System      string           `json:"system,omitempty"`
	MaxTokens   int              `json:"max_tokens,omitempty"`
	Stream      bool             `json:"stream,omitempty"`
	Temperature float64          `json:"temperature,omitempty"`
	TopP        float64          `json:"top_p,omitempty"`
	StopWords   []string         `json:"stop,omitempty"`
	Metadata    *json.RawMessage `json:"metadata,omitempty"`
}

type AnthropicMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// AnthropicMessageResponse 对应非流式响应
type AnthropicMessageResponse struct {
	ID         string               `json:"id"`
	Type       string               `json:"type"`
	Role       string               `json:"role"`
	Content    []AnthropicContent   `json:"content"`
	Model      string               `json:"model"`
	StopReason *string              `json:"stop_reason,omitempty"`
	StopSeq    *string              `json:"stop_sequence,omitempty"`
	Usage      AnthropicUsage       `json:"usage"`
}

type AnthropicContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type AnthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// AnthropicStreamEvent 用于序列化流式 SSE 事件的 data 部分
type AnthropicStreamEvent struct {
	Type        string               `json:"type"`
	Message     *AnthropicMessageResponse `json:"message,omitempty"`
	Index       *int                 `json:"index,omitempty"`
	ContentBlock *AnthropicContent   `json:"content_block,omitempty"`
	Delta       *AnthropicStreamDelta `json:"delta,omitempty"`
	Usage       *AnthropicUsage       `json:"usage,omitempty"`
}

type AnthropicStreamDelta struct {
	Type       string `json:"type,omitempty"`
	Text       string `json:"text,omitempty"`
	StopReason string `json:"stop_reason,omitempty"`
	StopSeq    string `json:"stop_sequence,omitempty"`
}

// HandleAnthropicMessages 处理 POST /v1/messages
func HandleAnthropicMessages(c *gin.Context, sender CommandSender) {
	var req AnthropicMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	prompt := lastAnthropicUserMessage(req.Messages)
	if prompt == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no user message found"})
		return
	}

	if req.Stream {
		handleAnthropicStreaming(c, sender, prompt, req.Model)
		return
	}

	resp, err := sender.SendRequest("chat", map[string]string{"message": prompt})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	content, usage := extractResponse(resp.Result)
	inputTokens := 0
	if usage != nil {
		inputTokens = usage.PromptTokens
	}

	stopReason := "end_turn"

	result := AnthropicMessageResponse{
		ID:   fmt.Sprintf("msg_%d", time.Now().UnixNano()),
		Type: "message",
		Role: "assistant",
		Content: []AnthropicContent{
			{Type: "text", Text: content},
		},
		Model:      req.Model,
		StopReason: &stopReason,
		Usage: AnthropicUsage{
			InputTokens:  inputTokens,
			OutputTokens: len(strings.Fields(content)) + 1,
		},
	}

	c.JSON(http.StatusOK, result)
}

func handleAnthropicStreaming(c *gin.Context, sender CommandSender, prompt string, model string) {
	resp, err := sender.SendRequest("chat", map[string]string{"message": prompt})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	content, usage := extractResponse(resp.Result)

	id := fmt.Sprintf("msg_%d", time.Now().UnixNano())
	inputTokens := 0
	if usage != nil {
		inputTokens = usage.PromptTokens
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	writeAnthropicSSE := func(event string, data interface{}) {
		b, _ := json.Marshal(data)
		fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event, b)
		c.Writer.Flush()
	}

	// 1. message_start
	writeAnthropicSSE("message_start", AnthropicStreamEvent{
		Type: "message_start",
		Message: &AnthropicMessageResponse{
			ID:   id,
			Type: "message",
			Role: "assistant",
			Content: []AnthropicContent{
				{Type: "text", Text: ""},
			},
			Model:      model,
			StopReason: nil,
			Usage: AnthropicUsage{
				InputTokens:  inputTokens,
				OutputTokens: 0,
			},
		},
	})

	// 2. content_block_start
	index := 0
	writeAnthropicSSE("content_block_start", AnthropicStreamEvent{
		Type:  "content_block_start",
		Index: &index,
		ContentBlock: &AnthropicContent{
			Type: "text",
			Text: "",
		},
	})

	// 3. content_block_delta (逐字/逐段发送)
	if content != "" {
		// 按字符发送流式 delta
		for _, r := range content {
			writeAnthropicSSE("content_block_delta", AnthropicStreamEvent{
				Type:  "content_block_delta",
				Index: &index,
				Delta: &AnthropicStreamDelta{
					Type: "text_delta",
					Text: string(r),
				},
			})
		}
	}

	// 4. content_block_stop
	writeAnthropicSSE("content_block_stop", AnthropicStreamEvent{
		Type:  "content_block_stop",
		Index: &index,
	})

	// 5. message_delta
	stopReason := "end_turn"
	writeAnthropicSSE("message_delta", AnthropicStreamEvent{
		Type: "message_delta",
		Delta: &AnthropicStreamDelta{
			StopReason: stopReason,
		},
		Usage: &AnthropicUsage{
			OutputTokens: len(strings.Fields(content)) + 1,
		},
	})

	// 6. message_stop
	writeAnthropicSSE("message_stop", AnthropicStreamEvent{
		Type: "message_stop",
	})
}

// lastAnthropicUserMessage 从 Anthropic 消息列表中提取最后一条 user 消息内容
func lastAnthropicUserMessage(messages []AnthropicMsg) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return messages[i].Content
		}
	}
	return ""
}


