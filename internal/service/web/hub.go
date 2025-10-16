// FILE: internal/service/web/hub.go
package web

import (
	"encoding/json"
	"github.com/gorilla/websocket"
	"liuproxy_nexus/internal/shared/logger"
	"net/http"
	"sync"
	"time"
)

// TrafficLogEntry 定义了单条流量日志的结构
type TrafficLogEntry struct {
	Timestamp   time.Time `json:"timestamp"`
	ClientIP    string    `json:"client_ip"`
	Protocol    string    `json:"protocol"`
	Destination string    `json:"destination"`
	Action      string    `json:"action"`
	Target      string    `json:"target,omitempty"`
}

// DashboardStats 定义了仪表盘所需的实时统计数据
type DashboardStats struct {
	Timestamp         time.Time `json:"timestamp"`
	ActiveConnections int64     `json:"active_connections"`
	UplinkRate        uint64    `json:"uplink_rate"`   // bytes per second
	DownlinkRate      uint64    `json:"downlink_rate"` // bytes per second
}

// WebSocketMessage 定义了 WebSocket 消息的通用格式
type WebSocketMessage struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

// Hub maintains the set of active clients and broadcasts messages to the
// clients.
type Hub struct {
	clients    map[*websocket.Conn]bool
	broadcast  chan []byte
	register   chan *websocket.Conn
	unregister chan *websocket.Conn
	mu         sync.Mutex
}

func NewHub() *Hub {
	return &Hub{
		broadcast:  make(chan []byte),
		register:   make(chan *websocket.Conn),
		unregister: make(chan *websocket.Conn),
		clients:    make(map[*websocket.Conn]bool),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case conn := <-h.register:
			h.mu.Lock()
			h.clients[conn] = true
			h.mu.Unlock()
			logger.Info().Str("remote_addr", conn.RemoteAddr().String()).Msg("WebSocket client registered.")
		case conn := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[conn]; ok {
				delete(h.clients, conn)
				conn.Close()
				logger.Info().Str("remote_addr", conn.RemoteAddr().String()).Msg("WebSocket client unregistered.")
			}
			h.mu.Unlock()
		case message := <-h.broadcast:
			h.mu.Lock()
			for conn := range h.clients {
				err := conn.WriteMessage(websocket.TextMessage, message)
				if err != nil {
					logger.Warn().Err(err).Str("remote_addr", conn.RemoteAddr().String()).Msg("Error writing to websocket client.")
					// Assume client is disconnected, let the read pump handle unregistering
				}
			}
			h.mu.Unlock()
		}
	}
}

// BroadcastStatusUpdate 广播状态更新消息
func (h *Hub) BroadcastStatusUpdate() {
	logger.Debug().Msg("Hub: Broadcasting status update to all clients.")
	msg := WebSocketMessage{Type: "status_update", Data: nil}
	jsonMsg, _ := json.Marshal(msg)

	select {
	case h.broadcast <- jsonMsg:
	default:
		logger.Warn().Msg("Hub: Broadcast channel is full, skipping status update.")
	}
}

// BroadcastTrafficLog 广播单条流量日志
func (h *Hub) BroadcastTrafficLog(entry *TrafficLogEntry) {
	msg := WebSocketMessage{Type: "traffic_log", Data: entry}
	jsonMsg, err := json.Marshal(msg)
	if err != nil {
		logger.Error().Err(err).Msg("Hub: Failed to marshal traffic log entry")
		return
	}

	select {
	case h.broadcast <- jsonMsg:
	default:
		// Do not log warning for full channel here to avoid log spam
	}
}

// BroadcastDashboardUpdate 广播仪表盘的实时统计数据
func (h *Hub) BroadcastDashboardUpdate(stats *DashboardStats) {
	msg := WebSocketMessage{Type: "dashboard_update", Data: stats}
	jsonMsg, err := json.Marshal(msg)
	if err != nil {
		logger.Error().Err(err).Msg("Hub: Failed to marshal dashboard stats")
		return
	}

	select {
	case h.broadcast <- jsonMsg:
	default:
		// Do not log warning for full channel to avoid log spam
	}
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true }, // Allow all origins
}

// ServeWs handles websocket requests from the peer.
func ServeWs(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to upgrade websocket")
		return
	}
	hub.register <- conn

	// This is a read pump. It's needed to detect when a client closes the connection.
	go func() {
		defer func() {
			hub.unregister <- conn
		}()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					logger.Warn().Err(err).Msg("Unexpected websocket close error")
				}
				break
			}
		}
	}()
}
