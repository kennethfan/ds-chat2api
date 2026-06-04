package transport

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type Message struct {
	Type   string          `json:"type"`
	ID     string          `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *ErrorInfo      `json:"error,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
}

type ErrorInfo struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ClientInfo struct {
	ID          string    `json:"id"`
	ConnectedAt time.Time `json:"connected_at"`
	LastSeen    time.Time `json:"last_seen"`
}

type WsServer struct {
	upgrader    websocket.Upgrader
	extConn     *websocket.Conn
	clientID    string
	connectedAt time.Time
	lastPong    time.Time
	mu          sync.Mutex
	stopCh      chan struct{}
}

func NewWsServer() *WsServer {
	return &WsServer{
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		stopCh: make(chan struct{}),
	}
}

func (s *WsServer) Start(port string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleConn)

	addr := fmt.Sprintf(":%s", port)
	log.Printf("WebSocket listening on ws://localhost%s", addr)
	go func() {
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Fatal(err)
		}
	}()

}

func (s *WsServer) Stop() {
	close(s.stopCh)
}

func (s *WsServer) disconnect(conn *websocket.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.extConn == conn {
		log.Printf("Client %s disconnected (cleanup)", s.clientID)
		s.extConn.Close()
		s.extConn = nil
		s.clientID = ""
	}
}

// startHeartbeat 定期发送 WebSocket Ping 帧检测连接存活
// 浏览器会自动回复 Pong，WriteControl 失败说明连接已断开
func (s *WsServer) startHeartbeat(conn *websocket.Conn) {
	go func() {
		ticker := time.NewTicker(25 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.mu.Lock()
				if s.extConn != conn {
					s.mu.Unlock()
					return // 连接已被替换，退出旧 heartbeat
				}
				s.mu.Unlock()

				if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second)); err != nil {
					log.Printf("Client %s heartbeat failed: %v", s.clientID, err)
					s.disconnect(conn)
					return
				}
			case <-s.stopCh:
				return
			}
		}
	}()
}

func (s *WsServer) Connected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.extConn != nil
}

func (s *WsServer) ConnectedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.extConn != nil {
		return 1
	}
	return 0
}

func (s *WsServer) Stats() map[string]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	stats := map[string]interface{}{
		"connected": s.extConn != nil,
		"count":     0,
	}
	if s.extConn != nil {
		stats["count"] = 1
		stats["client_id"] = s.clientID
		stats["connected_at"] = s.connectedAt.Format(time.RFC3339)
		stats["uptime"] = time.Since(s.connectedAt).String()
		stats["last_seen"] = s.lastPong.Format(time.RFC3339)
	}
	return stats
}

func (s *WsServer) handleConn(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("upgrade error: %v", err)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.extConn != nil {
		log.Printf("Replacing existing connection (client %s)", s.clientID)
		s.extConn.Close()
	}
	s.extConn = conn
	s.clientID = s.nextID()
	s.connectedAt = time.Now()
	s.lastPong = time.Now()

	conn.SetPongHandler(func(string) error {
		s.mu.Lock()
		s.lastPong = time.Now()
		s.mu.Unlock()
		return nil
	})
	conn.SetCloseHandler(func(code int, text string) error {
		s.mu.Lock()
		if s.extConn == conn {
			log.Printf("Client %s disconnected (code=%d)", s.clientID, code)
			s.extConn = nil
			s.clientID = ""
		}
		s.mu.Unlock()
		return nil
	})

	log.Printf("✓ Client %s connected", s.clientID)
	go s.startHeartbeat(conn)
	go s.runTests()
}

func (s *WsServer) nextID() string {
	return uuid.New().String()
}

func (s *WsServer) SendRequest(method string, params interface{}) (Message, error) {
	id := s.nextID()
	var paramsRaw json.RawMessage
	if params != nil {
		paramsRaw, _ = json.Marshal(params)
	}

	// 持锁写入，然后释放锁等待响应，避免阻塞其他操作（如 handleConn 处理重连）
	s.mu.Lock()
	if s.extConn == nil {
		s.mu.Unlock()
		return Message{}, fmt.Errorf("extension not connected")
	}

	msg := Message{
		Type:   "request",
		ID:     id,
		Method: method,
		Params: paramsRaw,
	}
	data, _ := json.Marshal(msg)
	log.Printf("[→] %s %s %s", id, method, string(paramsRaw))
	conn := s.extConn
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		s.extConn = nil
		s.clientID = ""
		s.mu.Unlock()
		conn.Close()
		return Message{}, fmt.Errorf("write error: %w", err)
	}
	s.mu.Unlock()

	// chat/newSession 涉及页面操作和等待 DeepSeek 响应，需要更长超时
	timeout := 30 * time.Second
	if method == "chat" || method == "newSession" {
		timeout = 120 * time.Second
	}

	// 不持锁等待响应，让 handleConn 等操作可以正常进行
	start := time.Now()
	conn.SetReadDeadline(time.Now().Add(timeout))
	_, respData, err := conn.ReadMessage()
	elapsed := time.Since(start)

	// 重取锁处理结果，期间检查连接是否已被替换
	s.mu.Lock()
	defer s.mu.Unlock()

	if err != nil {
		log.Printf("[✗] %s %s timeout=%v elapsed=%v err=%v", id, method, timeout, elapsed, err)
		if s.extConn == conn {
			s.extConn.Close()
			s.extConn = nil
			s.clientID = ""
		}
		return Message{}, fmt.Errorf("read error: %w", err)
	}
	log.Printf("[←] %s %s elapsed=%v", id, method, elapsed)

	// 如果连接已被替换，丢弃这条过期响应
	if s.extConn != conn {
		log.Printf("[!] %s response from stale connection, discarded", id)
		return Message{}, fmt.Errorf("connection replaced")
	}

	s.lastPong = time.Now()

	var resp Message
	json.Unmarshal(respData, &resp)

	switch resp.Type {
	case "response":
		log.Printf("[←] %s result: %s", id, string(resp.Result))
	case "error":
		log.Printf("[←] %s error: [%s] %s", id, resp.Error.Code, resp.Error.Message)
	case "sse":
		log.Printf("[←] %s sse: %s", id, string(resp.Data))
	default:
		log.Printf("[←] %s unknown type: %s", id, respData)
	}

	return resp, nil
}

func (s *WsServer) runTests() {
	for s.extConn == nil {
		time.Sleep(500 * time.Millisecond)
	}
	time.Sleep(1 * time.Second)

	log.Println("\n═══════════════════════════════════════")
	log.Println("  Starting test sequence")
	log.Println("═══════════════════════════════════════\n")

	log.Println("--- 1. ping ---")
	s.SendRequest("ping", nil)

	log.Println("\n--- 2. getStatus ---")
	s.SendRequest("getStatus", nil)

	log.Println("\n═══════════════════════════════════════")
	log.Println("  Test sequence complete")
	log.Println("═══════════════════════════════════════\n")
}
