package net

import (
	"liuproxy_nexus/internal/xray_core/common/errors"
	"strconv"
)

// Port represents a network port in TCP and UDP protocol.
type Port uint16

// PortFromInt converts an integer to a Port.
// @error when the integer is not positive or larger then 65535
func PortFromInt(val uint32) (Port, error) {
	if val > 65535 {
		return Port(0), errors.NewError("invalid port range: ", val)
	}
	return Port(val), nil
}

// PortFromString converts a string to a Port.
// @error when the string is not an integer or the integral value is a not a valid Port.
func PortFromString(s string) (Port, error) {
	val, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return Port(0), errors.NewError("invalid port range: ", s)
	}
	return PortFromInt(uint32(val))
}

// Value return the corresponding uint16 value of a Port.
func (p Port) Value() uint16 {
	return uint16(p)
}

func (p Port) String() string {
	return strconv.Itoa(int(p))
}
