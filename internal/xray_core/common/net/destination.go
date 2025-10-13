package net

// Destination represents a network destination including address and protocol (tcp / udp).
type Destination struct {
	Address Address
	Port    Port
	Network Network
}

// TCPDestination creates a TCP destination with given address
func TCPDestination(address Address, port Port) Destination {
	return Destination{
		Network: Network_TCP,
		Address: address,
		Port:    port,
	}
}

// NetAddr returns the network address in this Destination in string form.
func (d Destination) NetAddr() string {
	addr := ""
	if d.Network == Network_TCP || d.Network == Network_UDP {
		addr = d.Address.String() + ":" + d.Port.String()
	} else if d.Network == Network_UNIX {
		addr = d.Address.String()
	}
	return addr
}

// String returns the strings form of this Destination.
func (d Destination) String() string {
	prefix := "unknown:"
	switch d.Network {
	case Network_TCP:
		prefix = "tcp:"
	case Network_UDP:
		prefix = "udp:"
	case Network_UNIX:
		prefix = "unix:"
	}
	return prefix + d.NetAddr()
}
