package handler

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"ds-chat2api/server/internal/codec"
)

type ParsedEvent struct {
	Type string `json:"type"`

	RequestMessageID  int     `json:"request_message_id,omitempty"`
	ResponseMessageID int     `json:"response_message_id,omitempty"`
	ModelType         string  `json:"model_type,omitempty"`
	UpdatedAt         float64 `json:"updated_at,omitempty"`
	ClickBehavior     string  `json:"click_behavior,omitempty"`
	AutoResume        bool    `json:"auto_resume,omitempty"`

	V interface{} `json:"v,omitempty"`
	P string      `json:"p,omitempty"`
	O string      `json:"o,omitempty"`
}

func HandleCommand(c *gin.Context, sender CommandSender) {
	var req struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	if req.Method == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "method is required"})
		return
	}

	resp, err := sender.SendRequest(req.Method, req.Params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	parsedEvents := parseSSEFromResult(resp.Result)

	body := gin.H{
		"type": resp.Type,
		"id":   resp.ID,
	}
	if resp.Result != nil {
		body["result"] = resp.Result
	}
	if resp.Error != nil {
		body["error"] = resp.Error
	}
	if parsedEvents != nil {
		body["parsed_events"] = parsedEvents
	}
	c.JSON(http.StatusOK, body)
}

// parseSSEFromResult 尝试从 result.data.response 中提取 SSE 文本并解析为事件列表。
// 如果 result 结构不符合预期或解析失败，返回 nil。
func parseSSEFromResult(result json.RawMessage) []ParsedEvent {
	var wrapper struct {
		Data struct {
			Response string `json:"response"`
		} `json:"data"`
	}
	if err := json.Unmarshal(result, &wrapper); err != nil {
		return nil
	}
	if wrapper.Data.Response == "" {
		return nil
	}

	events, err := codec.ParseDeepSeekEvents([]byte(wrapper.Data.Response))
	if err != nil || len(events) == 0 {
		return nil
	}

	items := make([]ParsedEvent, 0, len(events))
	begin := false
	for _, e := range events {
		event := toParsedEvent(e)
		if event.Type != "data" {
			continue
		}

		if begin {
			if event.O == "BATCH" {
				break
			}

			items = append(items, toParsedEvent(e))
			continue
		}

		if event.P == "response/fragments/-1/elapsed_secs" {
			begin = true
		}
	}
	return items
}

func toParsedEvent(e codec.DeepSeekEvent) ParsedEvent {
	p := ParsedEvent{Type: e.Type.String()}
	switch {
	case e.Ready != nil:
		p.RequestMessageID = e.Ready.RequestMessageID
		p.ResponseMessageID = e.Ready.ResponseMessageID
		p.ModelType = e.Ready.ModelType
	case e.Session != nil:
		p.UpdatedAt = e.Session.UpdatedAt
	case e.Close != nil:
		p.ClickBehavior = e.Close.ClickBehavior
		p.AutoResume = e.Close.AutoResume
	case e.Data != nil:
		if len(e.Data.V) > 0 {
			var v interface{}
			if err := json.Unmarshal(e.Data.V, &v); err == nil {
				p.V = v
			}
		}
		p.P = e.Data.Path
		p.O = e.Data.Op
	}
	return p
}
