package codec

import (
	"bytes"
	"errors"
)

// Event 表示一个解析后的 SSE 事件
type Event struct {
	Event string // 事件类型（空串表示匿名 data 事件）
	Data  []byte // data: 后的原始内容（已去除 "data: " 前缀）
}

// ParseSSE 解析原始 SSE 文本（[]byte），返回事件列表。
// 按 \n\n 分割事件，解析 event: 和 data: 行。
// 符合 SSE 规范（https://html.spec.whatwg.org/multipage/server-sent-events.html）。
func ParseSSE(raw []byte) ([]Event, error) {
	if len(raw) == 0 {
		return nil, errors.New("sse: empty input")
	}

	// 按 \n\n 或 \r\n\r\n 分割事件块
	blocks := bytes.Split(raw, []byte("\n\n"))
	// 也处理 \r\n 行尾
	if len(blocks) == 1 && bytes.Contains(raw, []byte("\r\n\r\n")) {
		blocks = bytes.Split(raw, []byte("\r\n\r\n"))
	}

	var events []Event
	for _, block := range blocks {
		block = bytes.TrimSpace(block)
		if len(block) == 0 {
			continue
		}

		e, err := parseEventBlock(block)
		if err != nil {
			return nil, err
		}
		if e != nil {
			events = append(events, *e)
		}
	}

	if len(events) == 0 {
		return nil, errors.New("sse: no events found")
	}

	return events, nil
}

// parseEventBlock 解析一个事件块（多个换行分隔的行）
func parseEventBlock(block []byte) (*Event, error) {
	lines := bytes.Split(block, []byte("\n"))
	// 也处理 \r\n 行尾
	if len(lines) == 1 {
		lines = bytes.Split(block, []byte("\r\n"))
	}

	e := &Event{}
	var dataBuf bytes.Buffer

	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		switch {
		case bytes.HasPrefix(line, []byte("event:")):
			// event: type
			val := trimPrefix(line, []byte("event:"))
			e.Event = string(bytes.TrimSpace(val))

		case bytes.HasPrefix(line, []byte("data:")):
			// data: payload
			val := trimPrefix(line, []byte("data:"))
			val = bytes.TrimSpace(val)
			if dataBuf.Len() > 0 {
				dataBuf.WriteByte('\n')
			}
			dataBuf.Write(val)

		case bytes.HasPrefix(line, []byte(":")):
			// 注释行，忽略
			continue

		default:
			// 忽略不识别的行
			continue
		}
	}

	if dataBuf.Len() == 0 {
		return nil, nil // 没有 data 的事件块忽略
	}

	e.Data = dataBuf.Bytes()
	return e, nil
}

// trimPrefix 移除前缀，返回剩余部分（不修改原 slice）
func trimPrefix(b, prefix []byte) []byte {
	if len(b) < len(prefix) {
		return b
	}
	return b[len(prefix):]
}
