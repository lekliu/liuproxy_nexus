package web

import (
	"encoding/json"
	"io"
	"liuproxy_gateway/internal/shared/globalstate"
	"liuproxy_gateway/internal/shared/logger"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/google/uuid"
	"liuproxy_gateway/internal/shared/settings"
	"liuproxy_gateway/internal/shared/types"
)

// ServerController defines the interface that the web handler uses to interact with the AppServer.
// This decouples the web package from the server package.
type ServerController interface {
	GetServerStates() map[string]*types.ServerState
	UpdateServerActiveState(id string, active bool) error
	ReloadStrategy() error
	SaveConfigToFile() error
	GetAllServerProfilesSorted() []*types.ServerProfile
	AddServerProfile(profile *types.ServerProfile) error
	UpdateServerProfile(id string, updatedProfile *types.ServerProfile) error
	DeleteServerProfile(id string) error
	DuplicateServerProfile(id string) (*types.ServerProfile, error)
	GetRecentClientIPs() []string
	GetRecentTargets() []string
	ApplyChanges() error
}

// --- 新的系统环境 API ---

// SystemEnvSettings 定义了要从环境变量中读取并返回给前端的设置
type SystemEnvSettings struct {
	TCPEnabled  bool   `json:"tcp_enabled"`
	UDPEnabled  bool   `json:"udp_enabled"`
	TProxyPort  string `json:"tproxy_port"`
	ExcludedIPs string `json:"excluded_ips"`
}

// HandleGetSystemEnv 处理 GET /api/system/env 请求
func (h *Handler) HandleGetSystemEnv(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 从环境变量读取值，并提供默认值
	tcpEnabled, _ := strconv.ParseBool(os.Getenv("TRANSPARENT_PROXY_TCP_ENABLED"))
	udpEnabled, _ := strconv.ParseBool(os.Getenv("TRANSPARENT_PROXY_UDP_ENABLED"))

	// 对于没有设置环境变量的情况，Go的ParseBool会返回false，我们需要校正为默认的true
	if os.Getenv("TRANSPARENT_PROXY_TCP_ENABLED") == "" {
		tcpEnabled = true
	}
	if os.Getenv("TRANSPARENT_PROXY_UDP_ENABLED") == "" {
		udpEnabled = true
	}

	tproxyPort := os.Getenv("TPROXY_PORT")
	if tproxyPort == "" {
		tproxyPort = "12345"
	}

	excludedIPs := os.Getenv("EXCLUDED_IPS")
	if excludedIPs == "" {
		excludedIPs = "0.0.0.0/8,10.0.0.0/8,127.0.0.0/8,169.254.0.0/16,172.16.0.0/12,192.168.0.0/16,224.0.0.0/4,240.0.0.0/4"
	}

	settings := SystemEnvSettings{
		TCPEnabled:  tcpEnabled,
		UDPEnabled:  udpEnabled,
		TProxyPort:  tproxyPort,
		ExcludedIPs: excludedIPs,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(settings)
}

type Handler struct {
	serversPath     string
	settingsManager *settings.SettingsManager // 新增
	controller      ServerController
	mu              sync.Mutex
}

func NewHandler(
	cfg *types.Config,
	serversPath string,
	settingsManager *settings.SettingsManager,
	controller ServerController,
) *Handler {
	return &Handler{
		serversPath:     serversPath,
		settingsManager: settingsManager,
		controller:      controller,
	}
}

// --- 新的统一配置 API ---

// HandleGetSettings 处理 GET /api/settings 请求
func (h *Handler) HandleGetSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	currentSettings := h.settingsManager.Get()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(currentSettings)
}

// HandleUpdateSettings 处理 POST /api/settings/{module} 请求
func (h *Handler) HandleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 从 URL 路径中提取模块名
	moduleKey := strings.TrimPrefix(r.URL.Path, "/api/settings/")
	if moduleKey == "" {
		http.Error(w, "Module key is missing in URL path", http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)
		return
	}

	// 将更新请求委托给 SettingsManager
	if err := h.settingsManager.Update(moduleKey, body); err != nil {
		// 根据错误类型返回不同的状态码
		if strings.Contains(err.Error(), "unknown settings module") {
			http.Error(w, err.Error(), http.StatusNotFound)
		} else if strings.Contains(err.Error(), "failed to parse JSON") {
			http.Error(w, err.Error(), http.StatusBadRequest)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"message": "Settings updated successfully"}`))
}

// HandleGetClients 处理 GET /api/clients 请求，返回可用的客户端IP列表。
func (h *Handler) HandleGetClients(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 1. 获取所有路由规则中已经配置的目标 (domain 和 dest_ip)
	currentSettings := h.settingsManager.Get()
	configuredValues := make(map[string]struct{})

	for _, rule := range currentSettings.Routing.Rules {
		// 我们只关心 domain 和 dest_ip 类型的规则值
		if rule.Type == string(settings.RuleTypeDomain) || rule.Type == string(settings.RuleTypeDestIP) {
			for _, value := range rule.Value {
				configuredValues[value] = struct{}{}
			}
		}
	}

	// 2. 获取最近在线的IP
	recentIPs := h.controller.GetRecentClientIPs()

	// 3. 过滤掉已经配置过的IP
	availableIPs := make([]string, 0)
	for _, recentIP := range recentIPs {
		if _, exists := configuredValues[recentIP]; !exists {
			availableIPs = append(availableIPs, recentIP)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(availableIPs)
}

// HandleGetRecentTargets (MODIFIED to filter existing rule values)
func (h *Handler) HandleGetRecentTargets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	availableTargets := h.controller.GetRecentTargets()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(availableTargets)
}

// HandleApplyChanges - 处理 POST /api/apply_changes 请求
func (h *Handler) HandleApplyChanges(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	logger.Info().Msg("[Handler] Received request to apply configuration changes.")

	if err := h.controller.ApplyChanges(); err != nil {
		http.Error(w, "Failed to trigger apply changes: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"message": "Applying changes in the background."}`))
}

func (h *Handler) HandleStatus(w http.ResponseWriter, r *http.Request) {
	// 1. 扩展匿名 StatusResponse 结构体，以包含流量统计
	type MetricsWithTraffic struct {
		ActiveConnections int64  `json:"activeConnections"`
		Latency           int64  `json:"latency"`
		Uplink            uint64 `json:"uplink"`
		Downlink          uint64 `json:"downlink"`
	}
	type StatusResponse struct {
		GlobalStatus string                         `json:"globalStatus"`
		RuntimeInfo  map[string]*types.ListenerInfo `json:"runtimeInfo"`
		HealthStatus map[string]types.HealthStatus  `json:"healthStatus"`
		Metrics      map[string]*MetricsWithTraffic `json:"metrics"`
		ExitIPs      map[string]string              `json:"exitIPs"`
	}
	//logger.Debug().Msg("[Handler] HandleStatus: Fetching current server states...")

	// 从统一的状态源获取所有服务器状态
	serverStates := h.controller.GetServerStates()

	// 手动构建前端需要的三个独立的 map
	runtimeInfo := make(map[string]*types.ListenerInfo)
	healthStatus := make(map[string]types.HealthStatus)
	metrics := make(map[string]*MetricsWithTraffic)
	exitIPs := make(map[string]string)

	for id, state := range serverStates {
		if state.Instance != nil {
			runtimeInfo[id] = state.Instance.GetListenerInfo()
		}
		healthStatus[id] = state.Health
		exitIPs[id] = state.ExitIP

		// 2. 填充流量数据
		traffic := types.TrafficStats{} // 默认值
		if state.Instance != nil {
			traffic = state.Instance.GetTrafficStats()
			// logger.Debug().Str("server_id", id).Uint64("uplink", traffic.Uplink).Uint64("downlink", traffic.Downlink).Msg("Fetching traffic stats for server.")
		}

		metrics[id] = &MetricsWithTraffic{
			ActiveConnections: state.Metrics.ActiveConnections,
			Latency:           state.Metrics.Latency,
			Uplink:            traffic.Uplink,
			Downlink:          traffic.Downlink,
		}
	}

	response := StatusResponse{
		GlobalStatus: globalstate.GlobalStatus.Get(),
		RuntimeInfo:  runtimeInfo,
		HealthStatus: healthStatus,
		Metrics:      metrics,
		ExitIPs:      exitIPs,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// HandleSetServerActiveState 保持不变
func (h *Handler) HandleSetServerActiveState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := r.URL.Query().Get("id")
	activeStr := r.URL.Query().Get("active")
	active, err := strconv.ParseBool(activeStr)
	if err != nil || id == "" {
		http.Error(w, "Invalid parameters", http.StatusBadRequest)
		return
	}

	logger.Info().Str("id", id).Bool("active", active).Msg("[Handler] Received request to set server active state")

	// 1. Update state in A-Zone and manage instance
	if err := h.controller.UpdateServerActiveState(id, active); err != nil {
		logger.Error().Err(err).Msg("Failed to update server active state")
		http.Error(w, "Failed to update server active state: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 2. Immediately respond to the client.
	w.WriteHeader(http.StatusOK)
}

// HandleServers (CRUD) 保持不变
func (h *Handler) handleServersCRUD(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	switch r.Method {
	case http.MethodGet:
		h.getServers(w, r)
	case http.MethodPost:
		h.addServer(w, r)
	case http.MethodPut:
		h.updateServer(w, r)
	case http.MethodDelete:
		h.deleteServer(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// HandleServers (CRUD) 保持不变
func (h *Handler) HandleServers(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	switch r.Method {
	case http.MethodGet:
		h.getServers(w, r)
	case http.MethodPost:
		h.addServer(w, r)
	case http.MethodPut:
		h.updateServer(w, r)
	case http.MethodDelete:
		h.deleteServer(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// HandleServerActions handles non-CRUD actions like duplicating a server.
func (h *Handler) HandleServerActions(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/servers/")
	parts := strings.Split(path, "/")

	// Expects path like: "{uuid}/duplicate"
	if len(parts) == 2 && parts[1] == "duplicate" {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id := parts[0]
		h.duplicateServer(w, r, id)
		return
	}
	http.NotFound(w, r)
}

func (h *Handler) duplicateServer(w http.ResponseWriter, r *http.Request, id string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if id == "" {
		http.Error(w, "Missing server ID", http.StatusBadRequest)
		return
	}

	newProfile, err := h.controller.DuplicateServerProfile(id)
	if err != nil {
		http.Error(w, "Failed to duplicate server: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(newProfile)
}

func (h *Handler) getServers(w http.ResponseWriter, r *http.Request) {
	profiles := h.controller.GetAllServerProfilesSorted()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(profiles)
}

func (h *Handler) addServer(w http.ResponseWriter, r *http.Request) {
	var newProfile types.ServerProfile
	if err := json.NewDecoder(r.Body).Decode(&newProfile); err != nil {
		http.Error(w, "Invalid JSON format", http.StatusBadRequest)
		return
	}
	newProfile.ID = uuid.New().String()
	newProfile.Active = false

	if err := h.controller.AddServerProfile(&newProfile); err != nil {
		http.Error(w, "Failed to add server: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func (h *Handler) updateServer(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "Missing server ID", http.StatusBadRequest)
		return
	}
	var updatedProfile types.ServerProfile
	if err := json.NewDecoder(r.Body).Decode(&updatedProfile); err != nil {
		http.Error(w, "Invalid JSON format", http.StatusBadRequest)
		return
	}
	updatedProfile.ID = id // Ensure ID is correct

	if err := h.controller.UpdateServerProfile(id, &updatedProfile); err != nil {
		http.Error(w, "Failed to update server: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *Handler) deleteServer(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "Missing server ID", http.StatusBadRequest)
		return
	}

	if err := h.controller.DeleteServerProfile(id); err != nil {
		http.Error(w, "Failed to delete server: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
