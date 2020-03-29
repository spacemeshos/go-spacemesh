// Package nattraversal provides tools for acquiring ports on a Network Address Translator (NAT).
package nattraversal

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/nattraversal/upnp"
	"os"
	"os/user"
)

// GetExternalIP returns a string formatted external IP address from a UPnP-enabled NAT on the network, if such device
// is discovered.
func GetExternalIP() (string, error) {
	// connect to router
	d, err := upnp.Discover()
	if err != nil {
		return "", fmt.Errorf("failed to connect to router: %v", err)
	}

	// discover external IP
	ip, err := d.ExternalIP()
	if err != nil {
		return "", fmt.Errorf("failed to discover external IP: %v", err)
	}

	return ip, nil
}

func getPortDesc() string {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	username := "unknown"
	current, err := user.Current()
	if err == nil {
		username = current.Username
	}
	portDesc := fmt.Sprintf("spacemesh:%s@%s", username, hostname)
	return portDesc
}

// UPNPGateway is the interface of a UPnP gateway. It supports forwarding a port and clearing a forwarding rule.
type UPNPGateway interface {
	Forward(port uint16, desc string) error
	Clear(port uint16) error
}

// DiscoverUPNPGateway returns a UPNPGateway if one is found on the network. This process is long - about 30 seconds and
// is expected to fail in some network setups.
func DiscoverUPNPGateway() (UPNPGateway, error) {
	return upnp.Discover()
}

// AcquirePortFromGateway sets up UDP and TCP forwarding rules for the given port on the given gateway.
func AcquirePortFromGateway(gateway UPNPGateway, port uint16) error {
	err := gateway.Forward(port, getPortDesc())
	if err != nil {
		return fmt.Errorf("failed to forward port %d: %v", port, err)
	}

	return nil
}
