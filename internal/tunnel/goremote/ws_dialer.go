package goremote

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"liuproxy_nexus/internal/shared"
)

// DialWS 负责为 GoRemote 策略建立一个 WebSocket 连接
func DialWS(ctx context.Context, urlStr string, header http.Header) (net.Conn, error) {
	//logger.Debug().Str("url", urlStr).Msg("[GoRemote Dialer] Dialing new WebSocket connection...")

	dialer := websocket.Dialer{
		HandshakeTimeout: 15 * time.Second,
	}

	wsConn, _, err := dialer.DialContext(ctx, urlStr, header)
	if err != nil {
		return nil, fmt.Errorf("goremote websocket dial failed: %w", err)
	}

	//logger.Debug().Str("remote_addr", wsConn.RemoteAddr().String()).Msg("[GoRemote Dialer] SUCCESS: WebSocket connection established.")
	return shared.NewWebSocketConnAdapter(wsConn), nil
}
