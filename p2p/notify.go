package p2p

import (
	"sync"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

type NotifyProtocol interface {
	Connected(peer.ID)
	Disconnected(peer.ID)
}

type PeerNotifier struct {
	mtx  sync.Mutex
	subs []NotifyProtocol
}

func NewPeerNotifier() *PeerNotifier {
	return &PeerNotifier{
		// subs: make([]NotifyProtocol),
	}
}

func (n *PeerNotifier) subscribe(notifiee NotifyProtocol) {
	n.mtx.Lock()
	defer n.mtx.Unlock()

	n.subs = append(n.subs, notifiee)
}

func (n *PeerNotifier) Listen(network.Network, ma.Multiaddr)      {} // called when network starts listening on an addr
func (n *PeerNotifier) ListenClose(network.Network, ma.Multiaddr) {} // called when network stops listening on an addr
func (n *PeerNotifier) Connected(_ network.Network, c network.Conn) {
	p := c.RemotePeer()
	n.mtx.Lock()
	defer n.mtx.Unlock()
	for _, sub := range n.subs {
		go sub.Connected(p) // notify subscribers. one slow subscriber shouldn't block others
	}
}

func (n *PeerNotifier) Disconnected(_ network.Network, c network.Conn) {
	p := c.RemotePeer()
	n.mtx.Lock()
	defer n.mtx.Unlock()
	for _, sub := range n.subs {
		go sub.Disconnected(p) // notify subscribers. one slow subscriber shouldn't block others
	}
}
