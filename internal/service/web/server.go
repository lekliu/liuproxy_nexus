package web

import (
	"embed"
	"fmt"
	"io/fs"
	"liuproxy_gateway/internal/shared/logger"
	"liuproxy_gateway/internal/shared/settings"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"

	"liuproxy_gateway/internal/shared/types"
)

//go:embed all:static
var staticFiles embed.FS

// --- DIAGNOSTIC HELPER: A listener that logs accepted connections ---
type loggingListener struct {
	net.Listener
}

func (l loggingListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err == nil {
		logger.Debug().Msgf(" [WebServer DIAGNOSTIC] Connection accepted from: %s ", conn.RemoteAddr())
	}
	return conn, err
}

// basicAuthMiddleware 检查 web_user 和 web_password 是否已配置。
// 如果配置了，它将强制执行 HTTP Basic Authentication。
func basicAuthMiddleware(next http.Handler, user, pass string) http.Handler {
	// 如果用户名或密码未设置，则不启用认证，直接返回原始处理器
	if user == "" || pass == "" {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != user || p != pass {
			w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte("Unauthorized.\n"))
			return
		}
		// 认证成功，继续处理请求
		next.ServeHTTP(w, r)
	})
}

func StartServer(
	wg *sync.WaitGroup,
	cfg *types.Config,
	serversPath string,
	settingsManager *settings.SettingsManager,
	controller ServerController,
	hub *Hub,
) {
	if cfg.LocalConf.WebPort <= 0 {
		log.Println("[WebServer] Web UI is disabled (web_port is 0 or not set).")
		return
	}

	handler := NewHandler(cfg, serversPath, settingsManager, controller)
	mux := http.NewServeMux()

	// --- 认证保护的 API ---
	webUser := cfg.LocalConf.WebUser
	webPassword := cfg.LocalConf.WebPassword

	// 旧的服务器管理 API
	mux.Handle("/api/servers", basicAuthMiddleware(http.HandlerFunc(handler.HandleServers), webUser, webPassword))
	mux.Handle("/api/servers/", basicAuthMiddleware(http.HandlerFunc(handler.HandleServerActions), webUser, webPassword))

	mux.Handle("/api/servers/set_active_state", basicAuthMiddleware(http.HandlerFunc(handler.HandleSetServerActiveState), webUser, webPassword))
	mux.Handle("/api/apply_changes", basicAuthMiddleware(http.HandlerFunc(handler.HandleApplyChanges), webUser, webPassword))

	// 统一配置管理 API
	mux.Handle("/api/settings", basicAuthMiddleware(http.HandlerFunc(handler.HandleGetSettings), webUser, webPassword))
	mux.Handle("/api/settings/", basicAuthMiddleware(http.HandlerFunc(handler.HandleUpdateSettings), webUser, webPassword)) // 捕获 /api/settings/{module}
	mux.Handle("/api/clients", basicAuthMiddleware(http.HandlerFunc(handler.HandleGetClients), webUser, webPassword))
	mux.Handle("/api/system/env", basicAuthMiddleware(http.HandlerFunc(handler.HandleGetSystemEnv), webUser, webPassword))
	mux.Handle("/api/recent_targets", basicAuthMiddleware(http.HandlerFunc(handler.HandleGetRecentTargets), webUser, webPassword))

	// --- WebSocket Endpoint (公开，无需认证) ---
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		ServeWs(hub, w, r)
	})

	// 公开的状态 API
	mux.HandleFunc("/api/status", handler.HandleStatus)

	// --- 静态文件和主页 ---
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Fatalf("Failed to create sub filesystem for static assets: %v", err)
	}
	fileServer := http.FileServer(http.FS(staticFS))
	mux.Handle("/static/", http.StripPrefix("/static/", fileServer))

	// 主页需要认证
	rootHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 路由 /settings 到 index.html
		if r.URL.Path == "/" || strings.HasPrefix(r.URL.Path, "/settings") || strings.HasPrefix(r.URL.Path, "/monitor") {
			index, err := staticFiles.ReadFile("static/index.html")
			if err != nil {
				http.Error(w, "Could not load index.html", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(index)
			return
		}
		http.NotFound(w, r)
	})
	mux.Handle("/", basicAuthMiddleware(rootHandler, webUser, webPassword))

	addr := fmt.Sprintf("0.0.0.0:%d", cfg.LocalConf.WebPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Printf("!!! FAILED to start Web UI on %s: %v", addr, err)
		return
	}

	logger.Info().Msgf("SUCCESS: Web UI is listening on http://%s", addr)

	wg.Add(1)
	go func() {
		defer wg.Done()
		// Wrap the original listener with our logging listener
		loggingL := loggingListener{Listener: listener}
		if err := http.Serve(loggingL, mux); err != nil && err != http.ErrServerClosed {
			log.Printf("Web server error: %v", err)
		}
		log.Println("Web server stopped.")
	}()
}
