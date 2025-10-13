// --- START OF COMPLETE REPLACEMENT for liuproxy_go/internal/shared/conn_adapter.go ---
package shared

import (
	"fmt"
	"net"
	"time"

	"github.com/gorilla/websocket"
)

// WebSocketConnAdapter 实现了 net.Conn 接口
type WebSocketConnAdapter struct {
	*websocket.Conn
	readBuffer *ThreadSafeBuffer
}

// NewWebSocketConnAdapter 是一个新的构造函数，供拨号器使用
func NewWebSocketConnAdapter(ws *websocket.Conn) net.Conn {
	return &WebSocketConnAdapter{
		Conn:       ws,
		readBuffer: NewThreadSafeBuffer(),
	}
}

// Read 方法实现了 io.Reader 接口。
func (wsc *WebSocketConnAdapter) Read(b []byte) (int, error) {
	if wsc.readBuffer.Len() == 0 {
		msgType, msg, err := wsc.Conn.ReadMessage()
		if err != nil {
			return 0, err
		}
		if msgType != websocket.BinaryMessage {
			return 0, fmt.Errorf("received non-binary message")
		}
		if _, err := wsc.readBuffer.Write(msg); err != nil {
			return 0, err
		}
	}
	return wsc.readBuffer.Read(b)
}

// Write 方法实现了 io.Writer 接口。
func (wsc *WebSocketConnAdapter) Write(b []byte) (int, error) {
	dataCopy := make([]byte, len(b))
	copy(dataCopy, b)
	err := wsc.Conn.WriteMessage(websocket.BinaryMessage, dataCopy)
	if err != nil {
		return 0, err
	}
	return len(b), nil
}

// Close, LocalAddr, RemoteAddr, Set*Deadline 方法保持不变
func (wsc *WebSocketConnAdapter) Close() error         { return wsc.Conn.Close() }
func (wsc *WebSocketConnAdapter) LocalAddr() net.Addr  { return wsc.Conn.LocalAddr() }
func (wsc *WebSocketConnAdapter) RemoteAddr() net.Addr { return wsc.Conn.RemoteAddr() }
func (wsc *WebSocketConnAdapter) SetDeadline(t time.Time) error {
	_ = wsc.Conn.SetReadDeadline(t)
	return wsc.Conn.SetWriteDeadline(t)
}
func (wsc *WebSocketConnAdapter) SetReadDeadline(t time.Time) error {
	return wsc.Conn.SetReadDeadline(t)
}
func (wsc *WebSocketConnAdapter) SetWriteDeadline(t time.Time) error {
	return wsc.Conn.SetWriteDeadline(t)
}

// --- END OF COMPLETE REPLACEMENT ---
