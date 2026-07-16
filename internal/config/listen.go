package config

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// ValidateListen enforces Cortex's local-only trust boundary.
func ValidateListen(address string) error {
	address = strings.TrimSpace(address)
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("listen address must be loopback host:port: %w", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("listen address has an invalid port")
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("listen address must use a loopback host")
	}
	return nil
}
