package node

import (
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/rand"
)

var localhost = net.IP{127, 0, 0, 1}

// ErrFailedToCreate is returned when we fail to create a node
var ErrFailedToCreate = errors.New("failed to create local test node")

// GenerateTestNode generates a local test node without persisting data to local store and with default config value.
func GenerateTestNode(t *testing.T) (LocalNode, *Info) {
	return GenerateTestNodeWithConfig(t)
}

// GenerateTestNodeWithConfig creates a local test node without persisting data to local store.
func GenerateTestNodeWithConfig(t *testing.T) (LocalNode, *Info) {

	tcpPort, err := GetUnboundedPort("tcp", 0)
	if err != nil {
		t.Error("failed to get a port to bind", err)
	}
	udpPort, err := GetUnboundedPort("udp", tcpPort)
	if err != nil {
		t.Error("failed to get a port to bind", err)
	}

	addr := net.TCPAddr{IP: net.ParseIP("0.0.0.0"), Port: tcpPort}

	var localNode LocalNode

	localNode, err = NewNodeIdentity()
	if err != nil {
		t.Error(ErrFailedToCreate)
		return emptyNode, nil
	}
	return localNode, &Info{localNode.PublicKey().Array(), addr.IP, uint16(tcpPort), uint16(udpPort)}
}

// GenerateRandomNodeData generates a remote random node data for testing.
func GenerateRandomNodeData() *Info {
	rand.Seed(time.Now().UnixNano())
	port := uint16(rand.Int31n(48127) + 1024)
	pub := p2pcrypto.NewRandomPubkey()
	return NewNode(pub, localhost, port, port)
}

// GenerateRandomNodesData generates remote nodes data for testing.
func GenerateRandomNodesData(n int) []*Info {
	res := make([]*Info, n)
	for i := 0; i < n; i++ {
		res[i] = GenerateRandomNodeData()
	}
	return res
}

// GetUnboundedPort returns a port that is for sure unbounded or an error
// 	optionalPort can be 0 to return a random port.
func GetUnboundedPort(protocol string, optionalPort int) (int, error) {
	if protocol == "tcp" {
		l, e := net.Listen("tcp", fmt.Sprintf(":%v", optionalPort))
		if e != nil {
			l, e = net.Listen("tcp", ":0")
			if e != nil {
				return 0, e
			}
		}
		defer l.Close()
		return l.Addr().(*net.TCPAddr).Port, nil
	}
	if protocol == "udp" {
		l, e := net.ListenUDP("udp", &net.UDPAddr{Port: optionalPort})
		if e != nil {
			l, e = net.ListenUDP("udp", &net.UDPAddr{Port: 0})
			if e != nil {
				return 0, e
			}
		}
		defer l.Close()
		return l.LocalAddr().(*net.UDPAddr).Port, nil
	}

	return 0, errors.New("unknown protocol")
}
