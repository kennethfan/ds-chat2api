package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"ds-chat2api/server/internal/codec"
)

// --- OpenAI Chat Completions 请求/响应类型 ---

type ChatCompletionRequest struct {
	Model       string         `json:"model"`
	Messages    []ChatMessage  `json:"messages"`
	Stream      bool           `json:"stream,omitempty"`
	MaxTokens   int            `json:"max_tokens,omitempty"`
	Temperature float64        `json:"temperature,omitempty"`
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatCompletionResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []ChatChoice `json:"choices"`
	Usage   *TokenUsage  `json:"usage,omitempty"`
}

type ChatChoice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

type TokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func HandleChatCompletions(c *gin.Context, sender CommandSender) {
	var req ChatCompletionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	prompt := lastUserMessage(req.Messages)
	if prompt == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no user message found"})
		return
	}

	if req.Stream {
		handleStreaming(c, sender, prompt, req.Model)
		return
	}

	resp, err := sender.SendRequest("chat", map[string]string{"message": prompt})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	content, usage := extractResponse(resp.Result)

	id := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	result := ChatCompletionResponse{
		ID:      id,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []ChatChoice{
			{
				Index:        0,
				Message:      ChatMessage{Role: "assistant", Content: content},
				FinishReason: "stop",
			},
		},
		Usage: usage,
	}

	c.JSON(http.StatusOK, result)
}

func handleStreaming(c *gin.Context, sender CommandSender, prompt string, model string) {
	resp, err := sender.SendRequest("chat", map[string]string{"message": prompt})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	content, usage := extractResponse(resp.Result)

	id := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	created := time.Now().Unix()

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	chunk := func(data map[string]interface{}) {
		b, _ := json.Marshal(data)
		fmt.Fprintf(c.Writer, "data: %s\n\n", b)
		c.Writer.Flush()
	}

	// 流式发送：逐字模拟 delta
	chunk(map[string]interface{}{
		"id":      id,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   model,
		"choices": []map[string]interface{}{
			{"index": 0, "delta": map[string]string{"role": "assistant"}, "finish_reason": nil},
		},
	})

	for _, r := range content {
		chunk(map[string]interface{}{
			"id":      id,
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   model,
			"choices": []map[string]interface{}{
				{"index": 0, "delta": map[string]string{"content": string(r)}, "finish_reason": nil},
			},
		})
	}

	chunk(map[string]interface{}{
		"id":      id,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   model,
		"choices": []map[string]interface{}{
			{"index": 0, "delta": struct{}{}, "finish_reason": "stop"},
		},
	})

	if usage != nil {
		chunk(map[string]interface{}{
			"id":      id,
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   model,
			"choices": []map[string]interface{}{},
			"usage":   usage,
		})
	}

	fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
	c.Writer.Flush()
}

func lastUserMessage(messages []ChatMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return messages[i].Content
		}
	}
	return ""
}

// extractResponse 从 SendRequest 的 result 中解析 SSE 并提取最终回答内容和 token 用量。
func extractResponse(result json.RawMessage) (string, *TokenUsage) {
	var wrapper struct {
		Data struct {
			Response string `json:"response"`
		} `json:"data"`
	}
	if err := json.Unmarshal(result, &wrapper); err != nil || wrapper.Data.Response == "" {
		return "", nil
	}

	events, err := codec.ParseDeepSeekEvents([]byte(wrapper.Data.Response))
	if err != nil || len(events) == 0 {
		return "", nil
	}

	var content strings.Builder
	var totalTokens int

	// 过滤逻辑与 parseSSEFromResult 一致：
	// 1. 只保留 data 事件
	// 2. 从 elapsed_secs patch 开始收集（跳过 THINK 片段）
	// 3. 遇到 BATCH 时停止收集
	// 4. 从 BATCH 中提取 accumulated_token_usage
	var begin bool

	for _, e := range events {
		if e.Type != codec.EventTypeData || e.Data == nil {
			continue
		}

		// 提取 token 用量
		if e.Data.Op == "BATCH" {
			if patches, err := e.Data.ParseBatchPatches(); err == nil {
				for _, p := range patches {
					if p.Path == "accumulated_token_usage" && len(p.Value) > 0 {
						json.Unmarshal(p.Value, &totalTokens)
					}
				}
			}
			break
		}

		if begin {
			if e.Data.IsTextFragment() {
				if text, err := e.Data.TextFragment(); err == nil {
					content.WriteString(text)
				}
			}
			continue
		}

		if e.Data.Path == "response/fragments/-1/elapsed_secs" {
			begin = true
		}
	}

	text := content.String()

	var usage *TokenUsage
	if totalTokens > 0 {
		usage = &TokenUsage{TotalTokens: totalTokens}
		textLen := len(text)
		promptTokens := totalTokens - textLen/4
		if promptTokens < 0 {
			promptTokens = totalTokens / 3
		}
		usage.PromptTokens = promptTokens
		usage.CompletionTokens = totalTokens - promptTokens
	}

	return text, usage
}
