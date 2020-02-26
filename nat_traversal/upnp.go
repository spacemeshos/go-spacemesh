package nat_traversal

import (
	"fmt"
	"gitlab.com/NebulousLabs/go-upnp"
	"os"
	"os/user"
)

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

type UpnpGateway interface {
	Forward(port uint16, desc string) error
	Clear(port uint16) error
}

func DiscoverUPnPGateway() (UpnpGateway, error) {
	return upnp.Discover()
}

func AcquirePortFromGateway(gateway UpnpGateway, port uint16) error {
	err := gateway.Forward(port, getPortDesc())
	if err != nil {
		return fmt.Errorf("failed to forward port %d: %v", port, err)
	}

	return nil
}
