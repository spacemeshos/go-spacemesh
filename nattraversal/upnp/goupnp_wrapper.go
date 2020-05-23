// Package upnp provides a simple and opinionated interface to UPnP-enabled
// routers, allowing users to forward ports and discover their external IP
// address. Specific quirks:
//
// - When attempting to discover UPnP-enabled routers on the network, only the
// first such router is returned. If you have multiple routers, this may cause
// some trouble. But why would you do that?
//
// - Forwarded ports are always symmetric, e.g. the router's port 9980 will be
// mapped to the client's port 9980. This will be unacceptable for some
// purposes, but too bad. Symmetric mappings are the desired behavior 99% of
// the time, and they save a function argument.
//
// - TCP and UDP protocols are forwarded together.
//
// - Ports are forwarded permanently. Some other implementations lease a port
// mapping for a set duration, and then renew it periodically. This is nice,
// because it means mappings won't stick around after they've served their
// purpose. Unfortunately, some routers only support permanent mappings, so this
// package has chosen to support the lowest common denominator. To un-forward a
// port, you must use the Clear function (or do it manually).
//
// Once you've discovered your router, you can retrieve its address by calling
// its Location method. This address can be supplied to Load to connect to the
// router directly, which is much faster than calling Discover.
//
// Based on https://gitlab.com/NebulousLabs/go-upnp
//
// Instead of copying https://github.com/huin/goupnp this package uses it as an
// import. This means that, unlike NebulousLabs' implementation, we cannot pass
// a context to `Discover()`. On the other hand, we benefit from easy updates
// from upstream.
package upnp

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/rand"
	"net"
	"strings"
	"time"

	"github.com/huin/goupnp"
	"github.com/huin/goupnp/dcps/internetgateway1"
)

// An IGD provides an interface to the most commonly used functions of an
// Internet Gateway Device: discovering the external IP, and forwarding ports.
type IGD struct {
	// This interface is satisfied by the internetgateway1.WANIPConnection1
	// and internetgateway1.WANPPPConnection1 types.
	client interface {
		GetExternalIPAddress() (string, error)
		AddPortMapping(string, uint16, string, uint16, string, bool, string, uint32) error
		GetSpecificPortMappingEntry(string, uint16, string) (uint16, string, bool, string, uint32, error)
		DeletePortMapping(string, uint16, string) error
		GetServiceClient() *goupnp.ServiceClient
	}
}

// ExternalIP returns the router's external IP.
func (d *IGD) ExternalIP() (string, error) {
	return d.client.GetExternalIPAddress()
}

// IsForwardedTCP checks whether a specific TCP port is forwarded to this host
func (d *IGD) IsForwardedTCP(port uint16) (bool, error) {
	return d.checkForward(port, "TCP")
}

// IsForwardedUDP checks whether a specific UDP port is forwarded to this host
func (d *IGD) IsForwardedUDP(port uint16) (bool, error) {
	return d.checkForward(port, "UDP")
}

// checkForward checks whether a specific TCP or UDP port is forwarded to this host
func (d *IGD) checkForward(port uint16, proto string) (bool, error) {
	time.Sleep(time.Millisecond)
	_, _, enabled, _, _, err := d.client.GetSpecificPortMappingEntry("", port, proto)

	if err != nil {
		// 714 "NoSuchEntryInArray" means that there is no such forwarding
		if strings.Contains(err.Error(), "<errorCode>714</errorCode>") {
			return false, nil
		}
		return false, err
	}

	return enabled, nil
}

// Forward forwards the specified port, and adds its description to the
// router's port mapping table.
func (d *IGD) Forward(port uint16, desc string) error {
	ip, err := d.getInternalIP()
	if err != nil {
		return err
	}

	time.Sleep(time.Millisecond)
	err = d.client.AddPortMapping("", port, "TCP", port, ip, true, desc, 0)
	if err != nil {
		return err
	}

	time.Sleep(time.Millisecond)
	return d.client.AddPortMapping("", port, "UDP", port, ip, true, desc, 0)
}

// Clear un-forwards a port, removing it from the router's port mapping table.
func (d *IGD) Clear(port uint16) error {
	time.Sleep(time.Millisecond)
	tcpErr := d.client.DeletePortMapping("", port, "TCP")
	time.Sleep(time.Millisecond)
	udpErr := d.client.DeletePortMapping("", port, "UDP")

	// only return an error if both deletions failed
	if tcpErr != nil && udpErr != nil {
		return tcpErr
	}
	return nil
}

// Location returns the URL of the router, for future lookups (see Load).
func (d *IGD) Location() string {
	return d.client.GetServiceClient().Location.String()
}

// getInternalIP returns the user's local IP.
func (d *IGD) getInternalIP() (string, error) {
	host, _, _ := net.SplitHostPort(d.client.GetServiceClient().RootDevice.URLBase.Host)
	devIP := net.ParseIP(host)
	if devIP == nil {
		return "", errors.New("could not determine router's internal IP")
	}

	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}

	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			return "", err
		}

		for _, addr := range addrs {
			if x, ok := addr.(*net.IPNet); ok && x.Contains(devIP) {
				return x.IP.String(), nil
			}
		}
	}

	return "", errors.New("could not determine internal IP")
}

// Discover scans the local network for routers and returns the first
// UPnP-enabled router it encounters.  It will try up to 3 times to find a
// router, sleeping a random duration between each attempt.  This is to
// mitigate a race condition with many callers attempting to discover
// simultaneously.
func Discover() (*IGD, error) {
	// TODO: if more than one client is found, only return those on the same
	//  subnet as the user?
	maxTries := 3
	sleepTime := time.Millisecond * time.Duration(rand.Intn(5000))
	for try := 0; try < maxTries; try++ {
		pppclients, _, _ := internetgateway1.NewWANPPPConnection1Clients()
		if len(pppclients) > 0 {
			return &IGD{pppclients[0]}, nil
		}
		ipclients, _, _ := internetgateway1.NewWANIPConnection1Clients()
		if len(ipclients) > 0 {
			return &IGD{ipclients[0]}, nil
		}
		<-time.After(sleepTime)
		sleepTime *= 2
	}
	return nil, errors.New("no UPnP-enabled gateway found")
}
