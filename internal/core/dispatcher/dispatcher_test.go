package dispatcher

import (
	"context"
	"liuproxy_nexus/internal/shared/settings"
	"liuproxy_nexus/internal/shared/types"
	"net"
	"testing"
)

// MockTunnelStrategy is a mock for the strategy.TunnelStrategy interface
type MockTunnelStrategy struct {
	Listener *types.ListenerInfo
	Metrics  *types.Metrics
}

func (m *MockTunnelStrategy) Initialize() error                               { return nil }
func (m *MockTunnelStrategy) GetType() string                                 { return "mock" }
func (m *MockTunnelStrategy) CloseTunnel()                                    {}
func (m *MockTunnelStrategy) GetListenerInfo() *types.ListenerInfo            { return m.Listener }
func (m *MockTunnelStrategy) GetMetrics() *types.Metrics                      { return m.Metrics }
func (m *MockTunnelStrategy) UpdateServer(profile *types.ServerProfile) error { return nil }
func (m *MockTunnelStrategy) CheckHealth() error                              { return nil }

// mockStateProvider now only needs to implement GetServerStates.
type mockStateProvider struct {
	serverStates map[string]*types.ServerState
}

func (m *mockStateProvider) GetServerStates() map[string]*types.ServerState {
	return m.serverStates
}

// mockFailureReporter implements the FailureReporter for testing.
type mockFailureReporter struct {
	failures map[string]int
}

func (m *mockFailureReporter) ReportFailure(serverID string) {
	if m.failures == nil {
		m.failures = make(map[string]int)
	}
	m.failures[serverID]++
}
func (m *mockFailureReporter) ReportSuccess(serverID string) {
	if m.failures == nil {
		m.failures = make(map[string]int)
	}
	m.failures[serverID] = 0
}

// setupTestDispatcher is updated to use the new object model.
func setupTestDispatcher(
	stateProvider types.StateProvider,
	failureReporter types.FailureReporter,
	gatewaySettings *settings.GatewaySettings,
	routingRules *settings.RoutingSettings,
) *Dispatcher {
	d := New(gatewaySettings, stateProvider, failureReporter)

	if routingRules == nil {
		routingRules = &settings.RoutingSettings{
			Rules: []*settings.Rule{},
		}
	}

	if err := d.OnSettingsUpdate("routing", routingRules); err != nil {
		panic(err)
	}
	d.Start()
	return d
}

// --- Test Cases ---

func TestDispatch_LoadBalancing_AllHealthy(t *testing.T) {
	stateProvider := &mockStateProvider{
		serverStates: map[string]*types.ServerState{
			"server1": {
				Profile: &types.ServerProfile{ID: "server1", Remarks: "S1", Active: true},
				Instance: &MockTunnelStrategy{
					Listener: &types.ListenerInfo{Address: "127.0.0.1", Port: 1001},
				},
				Health:  types.StatusUp,
				Metrics: &types.Metrics{ActiveConnections: 1},
			},
			"server2": {
				Profile: &types.ServerProfile{ID: "server2", Remarks: "S2", Active: true},
				Instance: &MockTunnelStrategy{
					Listener: &types.ListenerInfo{Address: "127.0.0.1", Port: 1002},
				},
				Health:  types.StatusUp,
				Metrics: &types.Metrics{ActiveConnections: 5},
			},
		},
	}
	gatewaySettings := &settings.GatewaySettings{StickySessionMode: "disabled"}
	dispatcher := setupTestDispatcher(stateProvider, &mockFailureReporter{}, gatewaySettings, nil)

	sourceAddr, _ := net.ResolveTCPAddr("tcp", "192.168.1.10:12345")
	backendAddr, serverID, err := dispatcher.Dispatch(context.Background(), sourceAddr, "www.google.com:443")

	if err != nil {
		t.Fatalf("Dispatch() returned an error: %v", err)
	}
	if serverID != "server1" {
		t.Errorf("Expected serverID to be 'server1', but got '%s'", serverID)
	}
	if backendAddr != "127.0.0.1:1001" {
		t.Errorf("Expected backendAddr to be '127.0.0.1:1001', but got '%s'", backendAddr)
	}
}

func TestDispatch_LoadBalancing_OneUnhealthy(t *testing.T) {
	stateProvider := &mockStateProvider{
		serverStates: map[string]*types.ServerState{
			"server1": { // This one is unhealthy
				Profile:  &types.ServerProfile{ID: "server1", Remarks: "S1", Active: true},
				Instance: &MockTunnelStrategy{Listener: &types.ListenerInfo{Address: "127.0.0.1", Port: 1001}},
				Health:   types.StatusDown,
				Metrics:  &types.Metrics{ActiveConnections: 1},
			},
			"server2": { // This one is healthy
				Profile:  &types.ServerProfile{ID: "server2", Remarks: "S2", Active: true},
				Instance: &MockTunnelStrategy{Listener: &types.ListenerInfo{Address: "127.0.0.1", Port: 1002}},
				Health:   types.StatusUp,
				Metrics:  &types.Metrics{ActiveConnections: 5},
			},
		},
	}
	gatewaySettings := &settings.GatewaySettings{StickySessionMode: "disabled"}
	dispatcher := setupTestDispatcher(stateProvider, &mockFailureReporter{}, gatewaySettings, nil)

	sourceAddr, _ := net.ResolveTCPAddr("tcp", "192.168.1.10:12345")
	_, serverID, err := dispatcher.Dispatch(context.Background(), sourceAddr, "www.google.com:443")

	if err != nil {
		t.Fatalf("Dispatch() returned an error: %v", err)
	}
	if serverID != "server2" {
		t.Errorf("Expected to select healthy 'server2', but got '%s'", serverID)
	}
}

func TestDispatch_StickySession_Global(t *testing.T) {
	stateProvider := &mockStateProvider{
		serverStates: map[string]*types.ServerState{
			"server1": {
				Profile:  &types.ServerProfile{ID: "server1", Remarks: "S1", Active: true},
				Instance: &MockTunnelStrategy{Listener: &types.ListenerInfo{Address: "127.0.0.1", Port: 1001}},
				Health:   types.StatusUp,
				Metrics:  &types.Metrics{ActiveConnections: 1},
			},
			"server2": {
				Profile:  &types.ServerProfile{ID: "server2", Remarks: "S2", Active: true},
				Instance: &MockTunnelStrategy{Listener: &types.ListenerInfo{Address: "127.0.0.1", Port: 1002}},
				Health:   types.StatusUp,
				Metrics:  &types.Metrics{ActiveConnections: 5},
			},
		},
	}
	gatewaySettings := &settings.GatewaySettings{StickySessionMode: "global", StickySessionTTL: 5}
	dispatcher := setupTestDispatcher(stateProvider, &mockFailureReporter{}, gatewaySettings, nil)
	sourceAddr, _ := net.ResolveTCPAddr("tcp", "192.168.1.10:12345")

	// First request should pick server1 via load balancing
	_, serverID1, _ := dispatcher.Dispatch(context.Background(), sourceAddr, "www.youtube.com:443")
	if serverID1 != "server1" {
		t.Fatalf("Expected first dispatch to choose 'server1', got '%s'", serverID1)
	}

	// Update metrics to make server2 the better choice for load balancing
	stateProvider.serverStates["server1"].Metrics.ActiveConnections = 10
	stateProvider.serverStates["server2"].Metrics.ActiveConnections = 0

	// Second request should stick to server1
	_, serverID2, _ := dispatcher.Dispatch(context.Background(), sourceAddr, "www.youtube.com:443")
	if serverID2 != "server1" {
		t.Errorf("Expected sticky session to choose 'server1', but got '%s'", serverID2)
	}
}

func TestDispatch_Routing_VirtualStrategies(t *testing.T) {
	stateProvider := &mockStateProvider{
		serverStates: make(map[string]*types.ServerState), // No real servers needed
	}
	gatewaySettings := &settings.GatewaySettings{StickySessionMode: "disabled"}
	routingRules := &settings.RoutingSettings{
		Rules: []*settings.Rule{
			{Type: "domain", Value: []string{"ads.com"}, Target: "REJECT"},
			{Type: "domain", Value: []string{"local.dev"}, Target: "DIRECT"},
		},
	}
	dispatcher := setupTestDispatcher(stateProvider, &mockFailureReporter{}, gatewaySettings, routingRules)
	sourceAddr, _ := net.ResolveTCPAddr("tcp", "192.168.1.10:12345")

	addr, id, err := dispatcher.Dispatch(context.Background(), sourceAddr, "ads.com:443")
	if err != nil || addr != "REJECT" || id != "REJECT" {
		t.Errorf("Expected REJECT rule to match, got addr=%s, id=%s, err=%v", addr, id, err)
	}

	addr, id, err = dispatcher.Dispatch(context.Background(), sourceAddr, "local.dev:80")
	if err != nil || addr != "DIRECT" || id != "DIRECT" {
		t.Errorf("Expected DIRECT rule to match, got addr=%s, id=%s, err=%v", addr, id, err)
	}
}

// New test case for Sticky Session with health check
func TestDispatch_StickySession_FallbackOnUnhealthy(t *testing.T) {
	stateProvider := &mockStateProvider{
		serverStates: map[string]*types.ServerState{
			"server1": {
				Profile:  &types.ServerProfile{ID: "server1", Remarks: "S1", Active: true},
				Instance: &MockTunnelStrategy{Listener: &types.ListenerInfo{Address: "127.0.0.1", Port: 1001}},
				Health:   types.StatusUp,
				Metrics:  &types.Metrics{ActiveConnections: 1},
			},
			"server2": {
				Profile:  &types.ServerProfile{ID: "server2", Remarks: "S2", Active: true},
				Instance: &MockTunnelStrategy{Listener: &types.ListenerInfo{Address: "127.0.0.1", Port: 1002}},
				Health:   types.StatusUp,
				Metrics:  &types.Metrics{ActiveConnections: 5},
			},
		},
	}
	gatewaySettings := &settings.GatewaySettings{StickySessionMode: "global", StickySessionTTL: 5}
	dispatcher := setupTestDispatcher(stateProvider, &mockFailureReporter{}, gatewaySettings, nil)
	sourceAddr, _ := net.ResolveTCPAddr("tcp", "192.168.1.10:12345")

	// 1. First request, LB chooses server1 and creates a sticky record
	_, serverID1, _ := dispatcher.Dispatch(context.Background(), sourceAddr, "www.site.com:443")
	if serverID1 != "server1" {
		t.Fatalf("Expected first dispatch to choose 'server1', got '%s'", serverID1)
	}

	// 2. Mark server1 as unhealthy
	stateProvider.serverStates["server1"].Health = types.StatusDown

	// 3. Second request should ignore the sticky record for the unhealthy server1
	// and fall back to load balancing, choosing the only healthy one: server2.
	_, serverID2, err := dispatcher.Dispatch(context.Background(), sourceAddr, "www.site.com:443")
	if err != nil {
		t.Fatalf("Dispatch returned an error on fallback: %v", err)
	}
	if serverID2 != "server2" {
		t.Errorf("Expected to fall back to 'server2' after sticky target went down, but got '%s'", serverID2)
	}
}
