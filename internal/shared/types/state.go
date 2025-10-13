package types

// ListenerInfo holds the runtime listening info of a strategy instance.
type ListenerInfo struct {
	Address string
	Port    int
}

// Metrics holds the runtime performance metrics of a strategy instance.
type Metrics struct {
	ActiveConnections int64 `json:"activeConnections"`
	Latency           int64 `json:"latency"` // Latency in milliseconds (-1 for unknown/failed)
}
