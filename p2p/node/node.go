package node

import (
	"errors"
	"fmt"
	"github.com/btcsuite/btcutil/base58"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"net"
	"net/url"
	"regexp"
	"strconv"
)

// Scheme sets the URI scheme for the node string format.
const Scheme = "spacemesh"

// DiscoveryPortParam is the param used to define the port used for discovery.
const DiscoveryPortParam = "disc"

// ID is the public key represented as a fixed size 32 byte array.
type ID [32]byte

// PublicKey returns the public key as the PublicKey interface.
func (d ID) PublicKey() p2pcrypto.PublicKey {
	return p2pcrypto.PublicKeyFromArray(d)
}

// Bytes returns the ID as byte slice.
func (d ID) Bytes() []byte {
	return d[:]
}

// String returns a base58 string representation of the ID.
func (d ID) String() string {
	return base58.Encode(d[:])
}

// Info represents a p2p node that we know about.
type Info struct {
	ID
	IP            net.IP
	ProtocolPort  uint16 // TCP
	DiscoveryPort uint16 // UDP
}

// NewNode creates a new Info from public key, ip and ports.
func NewNode(id p2pcrypto.PublicKey, ip net.IP, proto, disc uint16) *Info {
	if ipv4 := ip.To4(); ipv4 != nil {
		ip = ipv4
	}
	return &Info{
		IP:            ip,
		ID:            id.Array(),
		ProtocolPort:  proto,
		DiscoveryPort: disc,
	}
}

/* NOTE: code below is from go-ethereum. modified for spacemesh needs*/

// Valid checks whether n is a valid complete node.
func (n Info) Valid() error {
	if n.IP == nil {
		return errors.New("no ip set to node")
	}
	if n.DiscoveryPort == 0 {
		return errors.New("missing UDP port")
	}
	if n.ProtocolPort == 0 {
		return errors.New("missing TCP port")
	}
	// TODO: consider uncommenting this.
	//if n.IP.IsMulticast() || n.IP.IsUnspecified() {
	//	return errors.New("invalid IP (multicast/unspecified)")
	//}

	// TODO: Validate pubkey
	if len(n.ID.Bytes()) != 32 {
		return errors.New("Invalid ID")
	}
	return nil
}

// The string representation of a Node is a URL.
// Please see ParseNode for a description of the format.
func (n Info) String() string {
	u := url.URL{Scheme: Scheme}

	if n.IP == nil {
		u.Host = n.ID.String()
	} else {
		addr := &net.TCPAddr{IP: n.IP, Port: int(n.ProtocolPort)}
		u.User = url.User(n.ID.String())
		u.Host = addr.String()
		if n.DiscoveryPort != n.ProtocolPort {
			u.RawQuery = fmt.Sprintf("%v=", DiscoveryPortParam) + strconv.Itoa(int(n.DiscoveryPort))
		}
	}
	return u.String()
}

var incompleteNodeURL = regexp.MustCompile(fmt.Sprintf("(?i)^(?:%v://)?([0-9a-f]+)$", Scheme))

// ParseNode parses a node designator.
//
// There are two basic forms of node designators
//   - incomplete nodes, which only have the public key (node ID)
//   - complete nodes, which contain the public key and IP/Port information
//
// For incomplete nodes, the designator must look like one of these
//
//    spacemesh://<base58 node id>
//    <hex node id>
//
// For complete nodes, the node ID is encoded in the username portion
// of the URL, separated from the host by an @ sign. The hostname can
// only be given as an IP address, DNS domain names are not allowed.
// The port in the host name section is the TCP listening port. If the
// TCP and UDP (discovery) ports differ, the UDP port is specified as
// query parameter "disc".
//
// In the following example, the node URL describes
// a node with IP address 10.3.58.6, TCP listening port 7513
// and UDP discovery port 7513.
//
//    spacemesh://<base58 node id>@10.3.58.6:7513?disc=7513
func ParseNode(rawurl string) (*Info, error) {
	if m := incompleteNodeURL.FindStringSubmatch(rawurl); m != nil {
		id, err := p2pcrypto.NewPrivateKeyFromBase58(m[1])
		if err != nil {
			return nil, fmt.Errorf("invalid node ID (%v)", err)
		}
		return NewNode(id, nil, 0, 0), nil
	}
	return parseComplete(rawurl)
}

func parseComplete(rawurl string) (*Info, error) {
	var (
		id               p2pcrypto.PublicKey
		ip               net.IP
		tcpPort, udpPort uint64
	)
	u, err := url.Parse(rawurl)
	if err != nil {
		return nil, err
	}
	if u.Scheme != Scheme {
		return nil, fmt.Errorf("invalid URL scheme, want '%v'", Scheme)
	}
	// Parse the Node ID from the user portion.
	if u.User == nil {
		return nil, errors.New("does not contain node ID")
	}
	if id, err = p2pcrypto.NewPrivateKeyFromBase58(u.User.String()); err != nil {
		return nil, fmt.Errorf("invalid node ID (%v)", err)
	}
	// Parse the IP address.
	host, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		return nil, fmt.Errorf("invalid host: %v", err)
	}
	if ip = net.ParseIP(host); ip == nil {
		return nil, errors.New("invalid IP address")
	}
	// Ensure the IP is 4 bytes long for IPv4 addresses.
	if ipv4 := ip.To4(); ipv4 != nil {
		ip = ipv4
	}
	// Parse the port numbers.
	if tcpPort, err = strconv.ParseUint(port, 10, 16); err != nil {
		return nil, errors.New("invalid port")
	}
	udpPort = tcpPort
	qv := u.Query()
	if qv.Get(DiscoveryPortParam) != "" {
		udpPort, err = strconv.ParseUint(qv.Get(DiscoveryPortParam), 10, 16)
		if err != nil {
			return nil, fmt.Errorf("invalid %v in query err=%v", DiscoveryPortParam, err)
		}
	}
	return NewNode(id, ip, uint16(tcpPort), uint16(udpPort)), nil
}
