package codec

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// sseResponse 匹配 sse.json 的顶层结构
type sseResponse struct {
	Result struct {
		Data struct {
			Response string `json:"response"`
		} `json:"data"`
	} `json:"result"`
}

func loadSSEFromJSON(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../../../sse.json")
	if err != nil {
		t.Fatal("read sse.json:", err)
	}
	var resp sseResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatal("unmarshal sse.json:", err)
	}
	if resp.Result.Data.Response == "" {
		t.Fatal("sse.json: result.data.response is empty")
	}
	return []byte(resp.Result.Data.Response)
}

func TestParseSSE_Count(t *testing.T) {
	raw := loadSSEFromJSON(t)
	events, err := ParseSSE(raw)
	if err != nil {
		t.Fatal("ParseSSE:", err)
	}
	if len(events) < 10 {
		t.Fatalf("expected >= 10 events, got %d", len(events))
	}
	t.Logf("total events: %d", len(events))
}

func TestParseSSE_NamedEvents(t *testing.T) {
	raw := loadSSEFromJSON(t)
	events, err := ParseSSE(raw)
	if err != nil {
		t.Fatal("ParseSSE:", err)
	}

	var foundReady, foundSession, foundClose bool
	for _, e := range events {
		switch e.Event {
		case "ready":
			foundReady = true
			if !strings.Contains(string(e.Data), "request_message_id") {
				t.Error("ready event missing request_message_id")
			}
		case "update_session":
			foundSession = true
		case "close":
			foundClose = true
		}
	}
	if !foundReady {
		t.Error("missing event: ready")
	}
	if !foundSession {
		t.Error("missing event: update_session")
	}
	if !foundClose {
		t.Error("missing event: close")
	}
}

func TestParseSSE_DataEvents(t *testing.T) {
	raw := loadSSEFromJSON(t)
	events, err := ParseSSE(raw)
	if err != nil {
		t.Fatal("ParseSSE:", err)
	}

	var dataCount int
	for _, e := range events {
		if e.Event == "" {
			dataCount++
			if len(e.Data) == 0 {
				t.Error("data event with empty Data")
			}
		}
	}
	if dataCount < 5 {
		t.Fatalf("expected >= 5 data events, got %d", dataCount)
	}
	t.Logf("data-only events: %d", dataCount)
}

func TestParseDeepSeekEvents_Types(t *testing.T) {
	raw := loadSSEFromJSON(t)
	events, err := ParseDeepSeekEvents(raw)
	if err != nil {
		t.Fatal("ParseDeepSeekEvents:", err)
	}

	var (
		ready, session, closeEvent int
		dataEvents                 int
		textFragments              int
		patches                    int
	)

	for _, e := range events {
		switch e.Type {
		case EventTypeReady:
			ready++
			if e.Ready == nil {
				t.Error("EventTypeReady but Ready is nil")
			}
			if e.Ready.RequestMessageID == 0 {
				t.Error("ReadyEvent.RequestMessageID is 0")
			}
		case EventTypeUpdateSession:
			session++
			if e.Session == nil {
				t.Error("EventTypeUpdateSession but Session is nil")
			}
		case EventTypeClose:
			closeEvent++
			if e.Close == nil {
				t.Error("EventTypeClose but Close is nil")
			}
		case EventTypeData:
			dataEvents++
			if e.Data == nil {
				t.Error("EventTypeData but Data is nil")
			}
			if e.Data.IsTextFragment() {
				textFragments++
				text, err := e.Data.TextFragment()
				if err != nil {
					t.Errorf("TextFragment() error: %v", err)
				}
				if text == "" {
					t.Error("empty text fragment")
				}
			}
			if e.Data.IsPatch() {
				patches++
			}
		}
	}

	t.Logf("ready=%d session=%d close=%d data=%d", ready, session, closeEvent, dataEvents)
	t.Logf("  data detail: textFragments=%d patches=%d", textFragments, patches)

	if ready != 1 {
		t.Errorf("expected 1 ready event, got %d", ready)
	}
	if dataEvents < 5 {
		t.Errorf("expected >= 5 data events, got %d", dataEvents)
	}
	if textFragments < 5 {
		t.Errorf("expected >= 5 text fragments, got %d", textFragments)
	}
}

func TestParseDeepSeekEvents_BatchPatch(t *testing.T) {
	raw := loadSSEFromJSON(t)
	events, err := ParseDeepSeekEvents(raw)
	if err != nil {
		t.Fatal("ParseDeepSeekEvents:", err)
	}

	var foundBatch bool
	for _, e := range events {
		if e.Type == EventTypeData && e.Data != nil && e.Data.IsBatch() {
			foundBatch = true
			patches, err := e.Data.ParseBatchPatches()
			if err != nil {
				t.Errorf("ParseBatchPatches error: %v", err)
			}
			if len(patches) == 0 {
				t.Error("BATCH patch list is empty")
			}
			for _, p := range patches {
				if p.Path == "" {
					t.Error("BATCH sub-patch has empty path")
				}
			}
			t.Logf("found BATCH with %d sub-patches", len(patches))
		}
	}
	if !foundBatch {
		t.Error("expected BATCH patch event not found")
	}
}

func TestParseSSE_EmptyInput(t *testing.T) {
	_, err := ParseSSE([]byte{})
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestParseDeepSeekEvents_ReadyEvent(t *testing.T) {
	raw := loadSSEFromJSON(t)
	events, err := ParseDeepSeekEvents(raw)
	if err != nil {
		t.Fatal("ParseDeepSeekEvents:", err)
	}

	for _, e := range events {
		if e.Type == EventTypeReady {
			t.Logf("ReadyEvent: request_message_id=%d response_message_id=%d model_type=%q",
				e.Ready.RequestMessageID, e.Ready.ResponseMessageID, e.Ready.ModelType)
			if e.Ready.ResponseMessageID <= e.Ready.RequestMessageID {
				t.Error("expected response_message_id > request_message_id")
			}
		}
	}
}
