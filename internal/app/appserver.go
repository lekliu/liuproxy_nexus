package app

import (
	"fmt"
	"github.com/google/uuid"
	"liuproxy_gateway/internal/core/dispatcher"
	"liuproxy_gateway/internal/core/gateway"
	"liuproxy_gateway/internal/core/health"
	"liuproxy_gateway/internal/firewall"
	"liuproxy_gateway/internal/service/web"
	"liuproxy_gateway/internal/shared/config"
	"liuproxy_gateway/internal/shared/logger"
	"liuproxy_gateway/internal/shared/settings"
	"liuproxy_gateway/internal/tunnel"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"liuproxy_gateway/internal/shared/types"
)

// AppState 包含 AppServer 的所有动态和半静态状态。
// 它现在只包含一个以 Server ID 为键的 ServerState map。
type AppState struct {
	Servers map[string]*types.ServerState
}

// AppServer is the application's main struct.
type AppServer struct {
	cfg         *types.Config
	iniPath     string
	serversPath string

	settingsManager *settings.SettingsManager

	serversFileLock sync.Mutex // NEW: Lock for servers.json read/write operations

	// configLock 保护 configState 的修改和后台操作
	configLock sync.RWMutex
	// configState is the "A Zone", for background configuration changes
	configState *AppState

	// workLock 保护 workState 指针的读取和交换
	workLock sync.RWMutex
	// workState is the "B Zone", for live traffic dispatching
	workState *AppState

	hub *web.Hub //  Hub 实例

	dispatcher         types.Dispatcher
	gateway            *gateway.Gateway
	transparentGateway *gateway.TransparentGateway // <-- 新增
	firewall           firewall.Firewall           // <-- 新增
	healthChecker      *health.Checker
	healthCheckTicker  *time.Ticker

	isMobileMode bool // 标记是否为移动模式

	waitGroup sync.WaitGroup
	stopOnce  sync.Once
}

// AppServer must implement StateManager 接口
var _ types.StateManager = (*AppServer)(nil)
var _ UDPListenerProvider = (*AppServer)(nil)

// UDPListenerProvider 是一个内部接口，用于解耦
type UDPListenerProvider interface {
	GetUDPListener() net.PacketConn
}

// NewForPC creates a new AppServer instance for PC/file-based mode.
func NewForPC(cfg *types.Config, iniPath, serversPath string) *AppServer {
	configDir := filepath.Dir(iniPath)
	settingsPath := filepath.Join(configDir, "settings.json")

	sm, err := settings.NewSettingsManager(settingsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: Failed to initialize settings manager: %v\n", err)
		os.Exit(1)
	}

	hub := web.NewHub() // 创建 Hub

	s := &AppServer{
		cfg:             cfg,
		iniPath:         iniPath,
		serversPath:     serversPath,
		settingsManager: sm,
		healthChecker:   health.New(false),
		hub:             hub, // 注入 Hub
		isMobileMode:    false,
		// 初始化 A 区和 B 区，防止空指针
		configState: &AppState{Servers: make(map[string]*types.ServerState)},
		workState:   &AppState{Servers: make(map[string]*types.ServerState)},
		// 降低健康检查频率，后续将改为配置驱动
		healthCheckTicker: time.NewTicker(30 * time.Second),
	}

	// 创建 Dispatcher 和 Firewall，并注入初始配置
	initialSettings := sm.Get()
	fw := firewall.NewEngine()
	s.firewall = fw

	// New Dispatcher 不再需要传递 s 作为 FailureReporter
	disp := dispatcher.New(initialSettings.Gateway, s)

	// 将 Dispatcher 注册为相关模块的订阅者
	sm.Register("gateway", disp)
	sm.Register("routing", disp)
	sm.Register("firewall", fw)

	// 初始化s.firewall
	if err := s.firewall.OnSettingsUpdate("firewall", initialSettings.Firewall); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: Failed to initialize firewall with initial settings: %v\n", err)
		os.Exit(1)
	}

	s.dispatcher = disp
	s.gateway = gateway.New(cfg.LocalConf.UnifiedPort, disp, s.hub)
	s.transparentGateway = gateway.NewTransparent(cfg.LocalConf.TProxyPort, fw, disp, s.hub) // <-- 新增: 创建透明网关实例

	return s
}

// NewForMobile creates a new AppServer instance for mobile/in-memory mode.
func NewForMobile(cfg *types.Config) *AppServer {
	// For mobile, settings manager runs in-memory without a file path.
	sm, err := settings.NewSettingsManager("")
	if err != nil {
		// This should theoretically not fail in memory mode, but we handle it.
		fmt.Fprintf(os.Stderr, "FATAL: Failed to initialize in-memory settings manager: %v\n", err)
		os.Exit(1) // A failure here is unrecoverable.
	}

	hub := web.NewHub()

	s := &AppServer{
		cfg:               cfg,
		iniPath:           "", // No file paths in mobile mode
		serversPath:       "",
		settingsManager:   sm,
		healthChecker:     health.New(true),
		hub:               hub,
		isMobileMode:      true,
		configState:       &AppState{Servers: make(map[string]*types.ServerState)},
		workState:         &AppState{Servers: make(map[string]*types.ServerState)},
		healthCheckTicker: time.NewTicker(30 * time.Second),
	}

	// In mobile mode, firewall is not used, so we create a dummy one.
	// Dispatcher is still needed for routing logic.
	initialSettings := sm.Get()
	fw := firewall.NewEngine() // Dummy engine, will be disabled by default settings
	s.firewall = fw

	disp := dispatcher.New(initialSettings.Gateway, s)
	s.dispatcher = disp
	s.gateway = gateway.New(cfg.LocalConf.UnifiedPort, disp, s.hub)
	// Transparent gateway is not used in mobile mode.
	s.transparentGateway = nil

	return s
}

// StartMobile is the server's entry point for mobile platforms.
// It initializes and runs the core services without the web UI or transparent gateway,
// using in-memory configurations provided as arguments.
// It returns the dynamically allocated port number for the unified gateway.
func (s *AppServer) StartMobile(profiles []*types.ServerProfile, routingRulesJson string) (int, error) {
	logger.Info().Msg("Starting server in 'mobile' mode...")

	if err := s.bootstrapFromMemory(profiles, routingRulesJson); err != nil {
		logger.Error().Err(err).Msg("Server bootstrap from memory failed (mobile mode)")
		return 0, err
	}

	s.dispatcher.(*dispatcher.Dispatcher).Start()

	s.waitGroup.Add(1)
	go s.statsLoop()

	s.waitGroup.Add(1)
	go s.healthCheckLoop()

	var unifiedPort int
	if s.cfg.LocalConf.UnifiedPort >= 0 { // Allow port 0 for dynamic allocation
		port, err := s.gateway.InitializeListener()
		if err != nil {
			logger.Error().Err(err).Msg("Gateway failed to initialize listener (mobile mode)")
			return 0, err
		}
		unifiedPort = port // Store the dynamically allocated port

		s.waitGroup.Add(1)
		go func() {
			defer s.waitGroup.Done()
			s.gateway.Serve()
		}()
	} else {
		logger.Warn().Msg("Gateway is disabled in mobile mode.")
		return 0, fmt.Errorf("unified_port is disabled")
	}

	// Do NOT start transparent gateway or web UI in mobile mode

	go s.hub.Run() // Hub is still needed for stats and logging, if ever needed

	return unifiedPort, nil
}

// Run is the server's entry point.
func (s *AppServer) Run() {
	logger.Info().Msg("Starting server in 'local' mode...")

	if err := s.loadConfigAndBootstrap(); err != nil {
		logger.Fatal().Err(err).Msg("Server bootstrap failed")
	}

	s.dispatcher.(*dispatcher.Dispatcher).Start()

	s.waitGroup.Add(1)
	go s.statsLoop()

	s.waitGroup.Add(1)
	go s.healthCheckLoop()

	if s.cfg.LocalConf.UnifiedPort > 0 {
		s.waitGroup.Add(1)
		go func() {
			defer s.waitGroup.Done()
			// 使用新的启动流程
			if _, err := s.gateway.InitializeListener(); err != nil {
				logger.Fatal().Err(err).Msg("Gateway failed to initialize listener")
			}
			s.gateway.Serve()
		}()
	} else {
		logger.Warn().Msg("Gateway is disabled.")
	}

	if s.cfg.LocalConf.TProxyPort > 0 { //  启动透明网关
		s.waitGroup.Add(1)
		go func() {
			defer s.waitGroup.Done()
			if err := s.transparentGateway.Start(); err != nil {
				// 使用 Fatal 级别，因为这是一个关键服务的失败
				logger.Fatal().Err(err).Msg("Transparent Gateway failed to start")
			}
		}()
	} else {
		logger.Warn().Msg("Transparent Gateway is disabled.")
	}

	go s.hub.Run() // 启动 Hub
	web.StartServer(&s.waitGroup, s.cfg, s.serversPath, s.settingsManager, s, s.hub)
	s.Wait()
}

// Stop gracefully shuts down the server.
func (s *AppServer) Stop() {
	s.stopOnce.Do(func() {
		//logger.Info().Msg("Stopping server...")
		if s.healthCheckTicker != nil {
			s.healthCheckTicker.Stop()
		}
		s.configLock.Lock()
		defer s.configLock.Unlock()

		if s.configState != nil && s.configState.Servers != nil {
			for id, state := range s.configState.Servers {
				if state.Instance != nil {
					logger.Info().Str("server_id", id).Msg("Closing strategy instance.")
					state.Instance.CloseTunnel()
					state.Instance = nil
				}
			}
		}

		if s.gateway != nil {
			s.gateway.Close()
		}
		if s.transparentGateway != nil {
			s.transparentGateway.Close()
		}
		//logger.Info().Msg("All strategies stopped.")
	})
}

// GetUDPListener 实现了 UDPListenerProvider 接口
// 它允许策略（如goremote）获取对主UDP监听器的引用，以便将返回的UDP包写回给正确的客户端
func (s *AppServer) GetUDPListener() net.PacketConn {
	if s.transparentGateway != nil {
		return s.transparentGateway.GetListener()
	}
	return nil
}

//	is the core instance lifecycle manager.
//
// It creates, starts, stops, and cleans up strategy instances based on the desired state in configState.
func (s *AppServer) manageInstances() {
	logger.Debug().Msg("[AppServer] Managing instances in A-Zone...")

	// 准备一个列表，存放需要被异步关闭的实例
	instancesToClose := make([]types.TunnelStrategy, 0)

	// Stop instances that are removed or deactivated
	for _, state := range s.configState.Servers {
		if !state.Profile.Active && state.Instance != nil {
			//logger.Info().Str("remarks", state.Profile.Remarks).Msg("Deactivating and closing instance.")
			instancesToClose = append(instancesToClose, state.Instance)
			state.Instance = nil
		}
	}

	// 异步、无锁地关闭实例
	go func(instances []types.TunnelStrategy) {
		for _, inst := range instances {
			logger.Debug().Str("type", inst.GetType()).Msg("Closing instance asynchronously.")
			inst.CloseTunnel()
		}
	}(instancesToClose)

	// Start instances that are new or activated
	for _, state := range s.configState.Servers {
		if state.Profile.Active && state.Instance == nil {
			//logger.Info().Str("remarks", state.Profile.Remarks).Msg("Activating and creating new strategy instance.")
			newInstance, err := tunnel.NewStrategy(s.cfg, []*types.ServerProfile{state.Profile}, s)
			if err != nil {
				logger.Error().Err(err).Str("remarks", state.Profile.Remarks).Msg("Failed to create strategy")
				state.Profile.Active = false // Mark as inactive on creation failure
				state.Health = types.StatusDown
				continue
			}
			state.Instance = newInstance

			if err := state.Instance.InitializeForGateway(); err != nil {
				logger.Error().Err(err).Str("remarks", state.Profile.Remarks).Msg("Failed to initialize strategy for Gateway mode")
				state.Instance.CloseTunnel()
				state.Instance = nil
				state.Profile.Active = false // Mark as inactive on init failure
				state.Health = types.StatusDown
				continue
			}
			// 初始化成功，直接设置为 Up
			state.Health = types.StatusUp
			//logger.Info().Str("remarks", state.Profile.Remarks).Msg("Instance initialized for Gateway mode successfully.")
		}
	}
	logger.Debug().Msg("[AppServer] Instance management complete.")
}

// bootstrapFromMemory orchestrates the startup sequence using in-memory profiles.
func (s *AppServer) bootstrapFromMemory(profiles []*types.ServerProfile, routingRulesJson string) error {
	//logger.Info().Msg("[AppServer] Starting memory bootstrap sequence...")

	if routingRulesJson != "" {
		// 直接将原始 JSON 数据传递给 Update 方法，由它负责解析、更新和通知
		if err := s.settingsManager.Update("routing", []byte(routingRulesJson)); err != nil {
			// 如果规则解析失败，这是一个致命错误，应阻止启动
			return fmt.Errorf("failed to apply routing rules from mobile client: %w", err)
		}
		//logger.Info().Msg("Routing rules from mobile client have been applied and dispatcher notified.")
	}

	s.configLock.Lock()
	if err := s.loadConfigFromMemory(profiles); err != nil {
		s.configLock.Unlock()
		return err
	}
	// manageInstances is safe, it doesn't do blocking file I/O
	s.manageInstances()
	s.configLock.Unlock()

	//logger.Info().Msg("[Bootstrap] Performing initial state publication...")
	if err := s.ReloadStrategy(); err != nil {
		return fmt.Errorf("initial state publication failed: %w", err)
	}

	//logger.Info().Msg("[AppServer] Memory bootstrap sequence completed.")
	return nil
}

// loadConfigFromMemory populates the configState (A-Zone) from a slice of profiles.
// This must be called under a write lock on configLock.
// It does NOT perform any file I/O.
func (s *AppServer) loadConfigFromMemory(profiles []*types.ServerProfile) error {
	//logger.Info().Msg("[AppServer] Loading server profiles from memory...")

	// Reset the current server map in configState
	s.configState.Servers = make(map[string]*types.ServerState)

	for _, profile := range profiles {
		// Profiles from mobile may not have an ID. Assign one if empty.
		// This ID is only for this runtime session.
		if profile.ID == "" {
			logger.Error().Str("remarks", profile.Remarks).Msg("Profile received from mobile is missing an ID. Skipping this profile.")
			continue // Skip this invalid profile
		}
		serverState := &types.ServerState{
			Profile: profile,
			Health:  types.StatusUnknown,
			Metrics: &types.Metrics{ActiveConnections: -1, Latency: -1},
		}
		s.configState.Servers[profile.ID] = serverState
	}

	logger.Info().Int("count", len(s.configState.Servers)).Msg("Server profiles loaded into A-Zone from memory.")
	return nil
}

// loadConfigAndBootstrap orchestrates the full startup sequence.
func (s *AppServer) loadConfigAndBootstrap() error {
	logger.Info().Msg("[AppServer] Starting bootstrap sequence...")

	// 1. Load profiles from servers.json into A-Zone, create instances
	s.configLock.Lock()
	if err := s.loadConfigFromFile(); err != nil {
		s.configLock.Unlock()
		return err
	}
	// manageInstances 包含阻塞IO，但这是在启动时，可以接受
	s.manageInstances()
	s.configLock.Unlock()

	// --- 在首次发布前，执行一次即时健康检查 ---
	//logger.Info().Msg("[Bootstrap] Performing initial health check...")
	s.runHealthChecks() // 这会更新 A-Zone (configState) 中的健康状态

	// 2. Perform the first publication to B-Zone.
	//logger.Info().Msg("[Bootstrap] Performing initial state publication...")
	if err := s.ReloadStrategy(); err != nil {
		return fmt.Errorf("initial state publication failed: %w", err)
	}

	//logger.Info().Msg("[AppServer] Bootstrap sequence completed.")
	return nil
}

// loadConfigFromFile reads servers.json and populates the initial configState (A-Zone).
// This must be called under a write lock on configLock.
func (s *AppServer) loadConfigFromFile() error {
	//logger.Info().Msg("[AppServer] Loading server profiles from file...")
	profiles, err := config.LoadServers(s.serversPath)
	if err != nil {
		return fmt.Errorf("failed to load server profiles from %s: %w", s.serversPath, err)
	}

	// Reset the current server map in configState
	s.configState.Servers = make(map[string]*types.ServerState)

	for _, profile := range profiles {
		if profile.ID == "" {
			profile.ID = uuid.New().String()
		}
		serverState := &types.ServerState{
			Profile: profile,
			Health:  types.StatusUnknown,
			Metrics: &types.Metrics{ActiveConnections: -1, Latency: -1},
		}
		s.configState.Servers[profile.ID] = serverState
	}

	// Save back immediately if any new IDs were generated
	// This avoids race conditions with subsequent UI operations.
	s.serversFileLock.Lock()
	defer s.serversFileLock.Unlock()
	if err := config.SaveServers(s.serversPath, profiles); err != nil {
		logger.Error().Err(err).Msg("Failed to save profiles after assigning new IDs")
	}

	//logger.Info().Int("count", len(s.configState.Servers)).Msg("Server profiles loaded into A-Zone.")
	return nil
}

func (s *AppServer) Wait() {
	s.waitGroup.Wait()
}

func (s *AppServer) healthCheckLoop() {
	defer s.waitGroup.Done()
	//logger.Info().Msg("[HealthChecker] Loop goroutine started. Waiting for initial tick...")

	// The initial check is now handled by loadConfigAndBootstrap.
	// The loop starts ticking immediately after its interval.
	for {
		select {
		case <-s.healthCheckTicker.C:
			//logger.Info().Msg("[HealthChecker] Tick received. Starting a health check cycle.")
			s.runHealthChecks()
		case <-s.done(): // Use a proper stop channel pattern
			//logger.Info().Msg("[HealthChecker] Loop goroutine received stop signal. Exiting.")
			return
		}
	}
}

func (s *AppServer) done() <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		s.stopOnce.Do(func() {
			close(ch)
		})
	}()
	return ch
}

func (s *AppServer) runHealthChecks() {
	logger.Debug().Msg("[HealthChecker] Starting periodic health check cycle...")
	// 1. Get a list of instances to check from the A-Zone (configState)
	s.configLock.RLock()
	instancesToCheck := make(map[string]types.TunnelStrategy)
	for id, state := range s.configState.Servers {
		if state.Profile.Active && state.Instance != nil {
			instancesToCheck[id] = state.Instance
		}
	}
	s.configLock.RUnlock()

	if len(instancesToCheck) == 0 {
		logger.Debug().Msg("[HealthChecker] No active instances to check.")
		return
	}

	// 2. Perform the actual checks (this can be slow)
	healthStatusMap, metricsCacheMap, exitIPsMap := s.healthChecker.Check(instancesToCheck)

	// 3. Lock A-Zone for writing and update the state
	s.configLock.Lock()
	var stateChanged bool
	for id, newHealth := range healthStatusMap {
		if state, ok := s.configState.Servers[id]; ok {
			oldLatency := state.Metrics.Latency
			newMetrics := metricsCacheMap[id]
			newExitIP := exitIPsMap[id]

			// A change is detected if health, latency, or exit IP changes.
			if state.Health != newHealth || (newMetrics != nil && oldLatency != newMetrics.Latency) || state.ExitIP != newExitIP {
				stateChanged = true
			}

			// Safely update the content of the existing Metrics struct, do NOT replace the pointer.
			if newMetrics != nil {
				state.Metrics.Latency = newMetrics.Latency
			}

			// Update Exit IP
			state.ExitIP = newExitIP

			if state.Health != newHealth {
				state.Health = newHealth
				//logger.Info().Str("server", state.Profile.Remarks).Interface("new_status", newHealth).Msg("Health status changed.")
			}
		}
	}
	s.configLock.Unlock()

	// 4. Log summary and publish if needed
	logger.Debug().
		Int("checked_count", len(healthStatusMap)).
		Bool("state_changed", stateChanged).
		Msg("[HealthChecker] Cycle complete.")

	if stateChanged {
		//logger.Info().Msg("[HealthChecker] Change detected, publishing updated state...")
		if err := s.ReloadStrategy(); err != nil {
			logger.Error().Err(err).Msg("Failed to reload state after health check cycle")
		}
	}
}

// statsLoop 定期聚合所有活动策略的统计数据并广播
func (s *AppServer) statsLoop() {
	defer s.waitGroup.Done()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	var lastUplink, lastDownlink uint64
	var lastTimestamp time.Time

	for {
		select {
		case <-ticker.C:
			s.workLock.RLock()
			// 如果 workState 未初始化，则跳过
			if s.workState == nil || s.workState.Servers == nil {
				s.workLock.RUnlock()
				continue
			}

			var totalConnections int64
			var currentUplink, currentDownlink uint64

			// 遍历 B 区（workState）中的所有服务器
			for _, state := range s.workState.Servers {
				if state.Profile.Active && state.Instance != nil {
					// 累加活动连接数
					metrics := state.Instance.GetMetrics()
					if metrics != nil && metrics.ActiveConnections >= 0 {
						totalConnections += metrics.ActiveConnections
					}
					// 累加总流量
					traffic := state.Instance.GetTrafficStats()
					currentUplink += traffic.Uplink
					currentDownlink += traffic.Downlink
				}
			}
			s.workLock.RUnlock()

			now := time.Now()
			var upRate, downRate uint64

			// 计算速率（如果不是第一次）
			if !lastTimestamp.IsZero() {
				elapsed := now.Sub(lastTimestamp).Seconds()
				if elapsed > 0 {
					upRate = uint64(float64(currentUplink-lastUplink) / elapsed)
					downRate = uint64(float64(currentDownlink-lastDownlink) / elapsed)
				}
			}

			// 更新上一次的数据
			lastUplink = currentUplink
			lastDownlink = currentDownlink
			lastTimestamp = now

			// 准备并广播数据
			stats := &web.DashboardStats{
				Timestamp:         now,
				ActiveConnections: totalConnections,
				UplinkRate:        upRate,
				DownlinkRate:      downRate,
			}
			s.hub.BroadcastDashboardUpdate(stats)

		case <-s.done():
			return
		}
	}
}

// GetIniPath returns the path to the ini config file.
func (s *AppServer) GetIniPath() string {
	return s.iniPath
}
