package nat_traversal

import (
	"fmt"
	"gitlab.com/NebulousLabs/go-upnp"
)

func GetExternalIp() (string, error) {
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

func AcquirePort(port uint16) (uint16, error) {
	// connect to router
	d, err := upnp.Discover()
	if err != nil {
		return 0, fmt.Errorf("failed to connect to router: %v", err)
	}

	// forward a port
	err = d.Forward(port, "upnp test")
	if err != nil {
		return 0, fmt.Errorf("failed to forward a port %d: %v", port, err)
	}

	return port, nil
}

func ReleasePort(port uint16) error {
	// connect to router
	d, err := upnp.Discover()
	if err != nil {
		return fmt.Errorf("failed to connect to router: %v", err)
	}

	// un-forward a port
	err = d.Clear(port)
	if err != nil {
		return fmt.Errorf("failed to un-forward a port %d: %v", port, err)
	}

	return nil
}
