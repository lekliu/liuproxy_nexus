package tunnel

import (
	"fmt"
	"liuproxy_gateway/internal/shared/types"
	"liuproxy_gateway/internal/tunnel/goremote"
	"liuproxy_gateway/internal/tunnel/httpproxy"
	"liuproxy_gateway/internal/tunnel/vless"
	"liuproxy_gateway/internal/tunnel/worker"
)

// NewStrategy is the factory function to create a new strategy based on the active profile.
// This is the single entry point for creating any strategy.
func NewStrategy(cfg *types.Config, profiles []*types.ServerProfile, stateManager types.StateManager) (types.TunnelStrategy, error) {
	var activeProfile *types.ServerProfile
	for _, p := range profiles {
		if p.Active {
			activeProfile = p
			break
		}
	}

	if activeProfile == nil {
		return nil, nil
	}

	switch activeProfile.Type {
	case "vless":
		return vless.NewVlessStrategy(cfg, activeProfile, stateManager)
	case "worker":
		return worker.NewWorkerStrategy(cfg, activeProfile, stateManager)
	case "goremote", "remote", "":
		return goremote.NewGoRemoteStrategy(cfg, activeProfile, stateManager)
	case "http":
		return httpproxy.NewHTTPStrategy(cfg, activeProfile, stateManager)
	default:
		return nil, fmt.Errorf("unknown or unsupported strategy type: '%s'", activeProfile.Type)
	}
}
