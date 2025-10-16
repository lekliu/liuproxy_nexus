package app

import (
	"fmt"
	"github.com/google/uuid"
	"liuproxy_nexus/internal/shared/config"
	"liuproxy_nexus/internal/shared/logger"
	"liuproxy_nexus/internal/shared/types"
	"sort"
)

// UpdateServerActiveState is called by the web handler to change a server's active status.
// It modifies the configState (A-Zone) and triggers instance management.
func (s *AppServer) UpdateServerActiveState(id string, active bool) error {
	s.configLock.Lock() // 直接获取写锁
	defer s.configLock.Unlock()

	state, ok := s.configState.Servers[id]
	if !ok {
		return fmt.Errorf("server with id %s not found in config state", id)
	}

	if state.Profile.Active == active {
		return nil // No change needed
	}

	state.Profile.Active = active
	logger.Info().Str("server", state.Profile.Remarks).Bool("active", active).Msg("Updated server active state in config (A-Zone). Changes will be applied on next sync.")

	// 【重要变更】移除 manageInstances 和 ReloadStrategy 调用
	// s.manageInstances()
	// s.ReloadStrategy()
	go func() {
		s.SaveConfigToFile()
	}()

	return nil
}

// AddServerProfile adds a new server to the configState (A-Zone).
func (s *AppServer) AddServerProfile(profile *types.ServerProfile) error {
	s.configLock.Lock()
	defer s.configLock.Unlock()

	if profile.ID == "" {
		return fmt.Errorf("profile must have an ID")
	}

	if _, exists := s.configState.Servers[profile.ID]; exists {
		return fmt.Errorf("server with id %s already exists", profile.ID)
	}

	serverState := &types.ServerState{
		Profile: profile,
		Health:  types.StatusUnknown,
		Metrics: &types.Metrics{ActiveConnections: -1, Latency: -1},
	}
	s.configState.Servers[profile.ID] = serverState
	logger.Info().Str("server", profile.Remarks).Msg("Added new server profile to config (A-Zone).")

	go func() {
		s.SaveConfigToFile()
	}()
	return nil
}

// UpdateServerProfile updates an existing server in the configState (A-Zone).
func (s *AppServer) UpdateServerProfile(id string, updatedProfile *types.ServerProfile) error {
	s.configLock.Lock()
	defer s.configLock.Unlock()

	state, ok := s.configState.Servers[id]
	if !ok {
		return fmt.Errorf("server with id %s not found", id)
	}

	// Preserve runtime state
	updatedProfile.Active = state.Profile.Active // Active status is managed separately
	state.Profile = updatedProfile
	logger.Info().Str("server", updatedProfile.Remarks).Msg("Updated server profile in config (A-Zone).")

	go func() {
		s.SaveConfigToFile()
	}()

	return nil
}

// DeleteServerProfile removes a server from the configState (A-Zone).
func (s *AppServer) DeleteServerProfile(id string) error {
	s.configLock.Lock()
	defer s.configLock.Unlock()

	state, ok := s.configState.Servers[id]
	if !ok {
		return fmt.Errorf("server with id %s not found", id)
	}

	// 如果实例存在，标记它以便异步关闭
	if state.Instance != nil {
		logger.Info().Str("server", state.Profile.Remarks).Msg("Marking instance for deletion and asynchronous closing.")
		go func(instanceToClose types.TunnelStrategy) {
			instanceToClose.CloseTunnel()
		}(state.Instance)
	}

	delete(s.configState.Servers, id)
	logger.Info().Str("server", state.Profile.Remarks).Msg("Deleted server profile from config (A-Zone).")

	go func() {
		s.SaveConfigToFile()
	}()

	return nil
}

// DuplicateServerProfile creates a copy of an existing server profile.
func (s *AppServer) DuplicateServerProfile(id string) (*types.ServerProfile, error) {
	s.configLock.Lock()
	defer s.configLock.Unlock()

	state, ok := s.configState.Servers[id]
	if !ok {
		return nil, fmt.Errorf("server with id %s not found", id)
	}

	// Create a value copy of the profile struct.
	originalProfile := state.Profile
	newProfile := *originalProfile

	// Modify the copy with a new ID, updated remarks, and set to inactive.
	newProfile.ID = uuid.New().String()
	newProfile.Remarks = originalProfile.Remarks + " - copy"
	newProfile.Active = false

	// Create a new server state for the duplicated profile.
	newState := &types.ServerState{
		Profile: &newProfile,
		Health:  types.StatusUnknown,
		Metrics: &types.Metrics{ActiveConnections: -1, Latency: -1},
	}
	s.configState.Servers[newProfile.ID] = newState

	logger.Info().Str("original_server", originalProfile.Remarks).Str("new_server", newProfile.Remarks).Msg("Duplicated server profile in config (A-Zone).")

	// Asynchronously save the updated configuration to file.
	go s.SaveConfigToFile()

	return &newProfile, nil
}

// GetAllServerProfilesSorted returns a sorted slice of all server profiles from the configState.
func (s *AppServer) GetAllServerProfilesSorted() []*types.ServerProfile {
	s.configLock.RLock()
	defer s.configLock.RUnlock()

	profiles := make([]*types.ServerProfile, 0, len(s.configState.Servers))
	for _, state := range s.configState.Servers {
		profiles = append(profiles, state.Profile)
	}

	// Sort by remarks to ensure stable order for the UI
	sort.Slice(profiles, func(i, j int) bool {
		return profiles[i].Remarks < profiles[j].Remarks
	})

	return profiles
}

// SaveConfigToFile persists the current configuration from A-Zone to servers.json.
func (s *AppServer) SaveConfigToFile() error {
	s.serversFileLock.Lock()
	defer s.serversFileLock.Unlock()

	s.configLock.RLock()
	profiles := make([]*types.ServerProfile, 0, len(s.configState.Servers))
	for _, state := range s.configState.Servers {
		profiles = append(profiles, state.Profile)
	}
	s.configLock.RUnlock()

	logger.Debug().Msg("Persisting configuration to servers.json directly from memory...")
	return config.SaveServers(s.serversPath, profiles)
}
