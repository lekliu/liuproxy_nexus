package tunnel

import (
	"fmt"
	"liuproxy_nexus/internal/shared/types"
	"liuproxy_nexus/internal/tunnel/goremote"
	"liuproxy_nexus/internal/tunnel/httpproxy"
	"liuproxy_nexus/internal/tunnel/socks5proxy"
	"liuproxy_nexus/internal/tunnel/vless"
	"liuproxy_nexus/internal/tunnel/worker"
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
		// Check the specific protocol for the "http" type server profile.
		switch activeProfile.ProxyProtocol {
		case "socks5":
			return socks5proxy.NewSOCKS5Strategy(cfg, activeProfile, stateManager)
		case "http", "": // Default to HTTP/HTTPS proxy
			return httpproxy.NewHTTPStrategy(cfg, activeProfile, stateManager)
		default:
			return nil, fmt.Errorf("unknown proxy_protocol for http type: '%s'", activeProfile.ProxyProtocol)
		}
	default:
		return nil, fmt.Errorf("unknown or unsupported strategy type: '%s'", activeProfile.Type)
	}
}
