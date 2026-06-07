package codec

import (
	"encoding/json"
	"fmt"
)

// DeepSeekEventType 区分 DeepSeek SSE 事件类型
type DeepSeekEventType int

const (
	EventTypeReady         DeepSeekEventType = iota // event: ready
	EventTypeUpdateSession                          // event: update_session
	EventTypeClose                                  // event: close
	EventTypeData                                   // 无 event 名的 data 事件
)

func (t DeepSeekEventType) String() string {
	switch t {
	case EventTypeReady:
		return "ready"
	case EventTypeUpdateSession:
		return "update_session"
	case EventTypeClose:
		return "close"
	case EventTypeData:
		return "data"
	default:
		return fmt.Sprintf("unknown(%d)", t)
	}
}

// DeepSeekEvent 是解析后的 DeepSeek 聊天 SSE 事件。
// 根据 Type 字段选择读取对应的具体事件结构。
type DeepSeekEvent struct {
	Type    DeepSeekEventType
	Raw     []byte               // 原始 JSON 数据
	Ready   *ReadyEvent          `json:"-"` // event: ready
	Session *UpdateSessionEvent  `json:"-"` // event: update_session
	Close   *CloseEvent          `json:"-"` // event: close
	Data   *DeepSeekDataEvent   `json:"-"` // 匿名 data 事件
}

// --- 命名事件类型 ---

// ReadyEvent event: ready 的数据
type ReadyEvent struct {
	RequestMessageID  int    `json:"request_message_id"`
	ResponseMessageID int    `json:"response_message_id"`
	ModelType         string `json:"model_type"`
}

// UpdateSessionEvent event: update_session 的数据
type UpdateSessionEvent struct {
	UpdatedAt float64 `json:"updated_at"`
}

// CloseEvent event: close 的数据
type CloseEvent struct {
	ClickBehavior string `json:"click_behavior"`
	AutoResume    bool   `json:"auto_resume"`
}

// --- data 事件类型 ---

// DeepSeekDataEvent 是匿名 data 事件的 JSON 负载。
// V 字段为 json.RawMessage，可能是字符串（文本片段）、对象（全量状态）或数组（BATCH 值）。
// 通过辅助方法判断具体类型。
type DeepSeekDataEvent struct {
	V    json.RawMessage `json:"v,omitempty"`
	Path string          `json:"p,omitempty"`
	Op   string          `json:"o,omitempty"`
}

// IsTextFragment 判断是否为纯文本追加事件：{"v":"text"}
func (d *DeepSeekDataEvent) IsTextFragment() bool {
	if len(d.V) == 0 || d.Path != "" || d.Op != "" {
		return false
	}
	return d.V[0] == '"'
}

// TextFragment 返回文本追加内容（仅当 IsTextFragment 为 true 时有意义）
func (d *DeepSeekDataEvent) TextFragment() (string, error) {
	if !d.IsTextFragment() {
		return "", fmt.Errorf("not a text fragment event")
	}
	var s string
	return s, json.Unmarshal(d.V, &s)
}

// IsStateUpdate 判断是否为全量状态更新：{"v":{"response":...}}
func (d *DeepSeekDataEvent) IsStateUpdate() bool {
	if len(d.V) == 0 || d.Path != "" || d.Op != "" {
		return false
	}
	return d.V[0] == '{'
}

// IsPatch 判断是否为 patch 操作（包含 p 字段）
func (d *DeepSeekDataEvent) IsPatch() bool {
	return d.Path != ""
}

// IsBatch 判断是否为批量 patch 操作
func (d *DeepSeekDataEvent) IsBatch() bool {
	return d.Op == "BATCH"
}

// ParseBatchPatches 解析 BATCH 操作的子 patch 列表
func (d *DeepSeekDataEvent) ParseBatchPatches() ([]PatchOp, error) {
	if !d.IsBatch() {
		return nil, fmt.Errorf("not a BATCH event")
	}
	var patches []PatchOp
	if err := json.Unmarshal(d.V, &patches); err != nil {
		return nil, fmt.Errorf("parse batch patches: %w", err)
	}
	return patches, nil
}

// PatchOp 表示单条 patch 操作
type PatchOp struct {
	Path  string          `json:"p"`
	Op    string          `json:"o,omitempty"`
	Value json.RawMessage `json:"v,omitempty"`
}

// ParseDeepSeekEvents 将原始 SSE 文本解析为 DeepSeek 事件列表。
// 同时进行 SSE 协议解析和 DeepSeek 业务类型识别。
func ParseDeepSeekEvents(raw []byte) ([]DeepSeekEvent, error) {
	rawEvents, err := ParseSSE(raw)
	if err != nil {
		return nil, fmt.Errorf("parse sse: %w", err)
	}

	events := make([]DeepSeekEvent, 0, len(rawEvents))
	for _, re := range rawEvents {
		de, err := convertEvent(re)
		if err != nil {
			return nil, fmt.Errorf("parse event %q: %w", re.Event, err)
		}
		events = append(events, de)
	}

	return events, nil
}

// convertEvent 将原始 SSE Event 转换为 DeepSeek 业务事件
func convertEvent(e Event) (DeepSeekEvent, error) {
	switch e.Event {
	case "ready":
		return parseNamedEvent(e, EventTypeReady, func(de *DeepSeekEvent, v *ReadyEvent) {
			de.Ready = v
		})
	case "update_session":
		return parseNamedEvent(e, EventTypeUpdateSession, func(de *DeepSeekEvent, v *UpdateSessionEvent) {
			de.Session = v
		})
	case "close":
		return parseNamedEvent(e, EventTypeClose, func(de *DeepSeekEvent, v *CloseEvent) {
			de.Close = v
		})
	case "":
		// 匿名 data 事件
		var data DeepSeekDataEvent
		if err := json.Unmarshal(e.Data, &data); err != nil {
			return DeepSeekEvent{}, fmt.Errorf("unmarshal data event: %w", err)
		}
		return DeepSeekEvent{
			Type: EventTypeData,
			Raw:  e.Data,
			Data: &data,
		}, nil
	default:
		// 未知事件类型，仍然保留原始数据
		return DeepSeekEvent{
			Type: EventTypeData,
			Raw:  e.Data,
		}, nil
	}
}

// parseNamedEvent 泛型辅助：解析命名事件并赋值到对应字段
func parseNamedEvent[T any](e Event, eventType DeepSeekEventType, assign func(*DeepSeekEvent, *T)) (DeepSeekEvent, error) {
	var v T
	if err := json.Unmarshal(e.Data, &v); err != nil {
		return DeepSeekEvent{}, fmt.Errorf("unmarshal %s event: %w", eventType, err)
	}
	de := DeepSeekEvent{
		Type: eventType,
		Raw:  e.Data,
	}
	assign(&de, &v)
	return de, nil
}
