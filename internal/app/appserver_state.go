package app

import (
	"liuproxy_gateway/internal/core/dispatcher"
	"liuproxy_gateway/internal/shared/logger"
	"liuproxy_gateway/internal/shared/settings"
	"liuproxy_gateway/internal/shared/types"
)

// deepCopyAppState performs a deep copy of the AppState, suitable for creating a read-only snapshot.
// Pointers to Profile and Instance are copied by value, which is intentional to avoid re-creating them.
func deepCopyAppState(original *AppState) *AppState {
	if original == nil {
		return nil
	}

	newState := &AppState{
		Servers: make(map[string]*types.ServerState, len(original.Servers)),
	}

	for id, serverState := range original.Servers {
		// Create a copy of the ServerState struct.
		stateCopy := *serverState
		// The Profile and Instance pointers are copied by value, which is what we want.
		// Metrics, however, should be a deep copy to prevent race conditions on metric updates.
		if serverState.Metrics != nil {
			metricsCopy := *serverState.Metrics
			stateCopy.Metrics = &metricsCopy
		}
		stateCopy.Health = serverState.Health
		newState.Servers[id] = &stateCopy
	}

	return newState
}

// ReloadStrategy is the lightweight "publisher" that copies the state from A-Zone (configState) to B-Zone (workState).
// This operation is fast and read-locks A-Zone, then write-locks B-Zone for an atomic swap.
func (s *AppServer) ReloadStrategy() error {
	logger.Debug().Msg("[AppServer] Publishing A-Zone state to B-Zone...")

	s.configLock.RLock()
	newState := deepCopyAppState(s.configState)
	s.configLock.RUnlock()

	s.workLock.Lock()
	s.workState = newState
	s.workLock.Unlock()

	// Log the final state that was just published to the workState for dispatcher use.
	finalWorkStateForLog := make(map[string]map[string]interface{})
	for _, state := range newState.Servers {
		finalWorkStateForLog[state.Profile.Remarks] = map[string]interface{}{
			"Health":   int(state.Health),
			"Instance": state.Instance != nil,
		}
	}
	logger.Debug().
		Interface("published_workState_summary", finalWorkStateForLog).
		Msg("[AppServer] ReloadStrategy finished and updated workState.")

	// After publishing, notify the dispatcher that the routing table might need an update
	// because backend health/availability could have changed.
	go func() {
		currentRoutingSettings := s.settingsManager.Get().Routing
		if disp, ok := s.dispatcher.(settings.ConfigurableModule); ok {
			if err := disp.OnSettingsUpdate("routing", currentRoutingSettings); err != nil {
				logger.Error().Err(err).Msg("Error notifying dispatcher of routing update after reload")
			}
		}
	}()

	//logger.Info().Int("count", len(newState.Servers)).Msg(">>> State published to dispatcher successfully.")
	return nil
}

// ApplyChanges - 应用所有暂存的配置变更
func (s *AppServer) ApplyChanges() error {
	//logger.Info().Msg("[AppServer] Applying configuration changes...")

	// 在一个独立的 goroutine 中执行耗时操作，以避免阻塞调用方（如HTTP handler）
	go func() {
		s.configLock.Lock()
		s.manageInstances()
		s.configLock.Unlock()

		// manageInstances 执行完毕后，再发布状态
		if err := s.ReloadStrategy(); err != nil {
			logger.Error().Err(err).Msg("Failed to reload strategy after applying changes.")
		}
	}()

	return nil
}

// 现 StateManager 接口
func (s *AppServer) SetServerStatusDown(serverID, reason string) {
	s.configLock.Lock()
	state, ok := s.configState.Servers[serverID]
	if !ok {
		s.configLock.Unlock()
		return
	}

	if state.Health == types.StatusDown {
		s.configLock.Unlock()
		return // 状态已经是 Down，无需重复操作
	}

	state.Health = types.StatusDown
	s.configLock.Unlock()

	logger.Warn().Str("server_id", serverID).Str("reason", reason).Msg("Server status set to DOWN due to connection failure.")

	// 立即将状态变更发布到工作区
	if err := s.ReloadStrategy(); err != nil {
		logger.Error().Err(err).Msg("Failed to reload strategy after setting server down.")
	}

	// 通知所有 WebSocket 客户端状态已更新
	s.hub.BroadcastStatusUpdate()
}

// GetServerStates implements the StateProvider interface.
func (s *AppServer) GetServerStates() map[string]*types.ServerState {
	s.workLock.RLock()
	newState := deepCopyAppState(s.workState)
	s.workLock.RUnlock()
	return newState.Servers
}

// GetRecentClientIPs implements the ServerController interface.
func (s *AppServer) GetRecentClientIPs() []string {
	if d, ok := s.dispatcher.(*dispatcher.Dispatcher); ok {
		return d.GetRecentClientIPs()
	}
	return []string{}
}

// GetRecentTargets implements the ServerController interface.
func (s *AppServer) GetRecentTargets() []string {
	if d, ok := s.dispatcher.(*dispatcher.Dispatcher); ok {
		// Get current routing rules from settings manager to pass them for filtering
		currentRules := s.settingsManager.Get().Routing.Rules
		return d.GetRecentTargets(currentRules)
	}
	return []string{}
}
