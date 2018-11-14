package gossip

import (
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/davecgh/go-spew/spew"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/message"
	"github.com/spacemeshos/go-spacemesh/p2p/net"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"sync"
	"time"
)

const NoResultsTimeout = 1 * time.Second
const PeerMessageQueueSize = 100


type Protocol interface {
	Broadcast(payload []byte) error
	Start() error
	AddPeer(node.Node, net.Connection)
	Shutdown()
}

type PeerSampler interface {
	SelectPeers(count int) []node.Node
}

type ConnectionFactory interface {
	GetConnection(address string, pk crypto.PublicKey) (net.Connection, error)
}

type NodeConPair struct {
	node.Node
	net.Connection
}

type Neighborhood struct {
	log.Log

	config config.SwarmConfig

	peers        map[string]*peer
	inpeers map[string]*peer

	morePeersReq chan struct{}

	inc chan NodeConPair

	// closed peers are reported here
	remove chan string

	// we make sure we don't send a message twice
	oldMessageMu sync.RWMutex
	oldMessageQ  map[string]struct{}

	ps PeerSampler

	cp ConnectionFactory

	shutdown chan struct{}

	peersMutex sync.RWMutex
	inpeersMutex sync.RWMutex
}

func NewNeighborhood(config config.SwarmConfig, ps PeerSampler, cp ConnectionFactory, log2 log.Log) *Neighborhood {
	return &Neighborhood{
		Log:          log2,
		config:       config,
		morePeersReq: make(chan struct{}, config.RandomConnections),
		peers:        make(map[string]*peer, config.RandomConnections),
		inpeers:        make(map[string]*peer, config.RandomConnections),
		oldMessageQ:  make(map[string]struct{}), // todo : remember to drain this
		ps:           ps,
		cp:           cp,
	}
}

var _ Protocol = new(Neighborhood)

type peer struct {
	log.Log
	node.Node
	disc          chan error
	connected     time.Time
	conn          net.Connection
	knownMessages map[string]struct{}
	msgQ          chan []byte
}

func makePeer(node2 node.Node, c net.Connection, log log.Log) *peer {
	return &peer{
		log,
		node2,
		make(chan error, 1),
		time.Now(),
		c,
		make(map[string]struct{}),
		make(chan []byte, PeerMessageQueueSize),
	}
}

func (p *peer) send(message []byte) error {
	if p.conn == nil || p.conn.Session() == nil {
		return fmt.Errorf("the connection does not exist for this peer")
	}
	return p.conn.Send(message)
}

func (p *peer) addMessage(msg []byte) error {
	// dont do anything if this peer know this msg
	if _, ok := p.knownMessages[hex.EncodeToString(msg)]; ok {
		return errors.New("already got this msg")
	}

	// check if connection and session are ok
	c := p.conn
	session := c.Session()
	if c == nil || session == nil {
		// todo: refresh Neighborhood or create session
		return errors.New("no session")
	}

	data, err := message.PrepareMessage(session, msg)

	if err != nil {
		return err
	}

	select {
	case p.msgQ <- data:

	default:
		return errors.New("Q was full")

	}

	return nil
}

func (p *peer) start(dischann chan string) {
	// check on new peers if they need something we have
	//c := make(chan []string)
	//t := time.NewTicker(time.Second * 5)
	for {
		select {
		case m := <-p.msgQ:
			err := p.send(m)
			if err != nil {
				// todo: handle errors
				log.Error("Failed sending message to this peer %v", p.Node.PublicKey().String())
				p.disc <- err
			}
		case d := <-p.disc:
			log.Error("peer disconnected %v", d)
			if dischann != nil {
				dischann <- p.PublicKey().String()
			}
			return
		}

	}
}

func (s *Neighborhood) Shutdown() {
	// no need to shutdown con, conpool will do so in a shutdown. the morepeerreq won't work
	close(s.shutdown)
}

func (s *Neighborhood) Peer(pubkey string) (node.Node, net.Connection) {
	s.peersMutex.RLock()
	p, ok := s.peers[pubkey]
	s.peersMutex.RUnlock()
	if ok {
		return p.Node, p.conn
	}
	return node.EmptyNode, nil

}

// the actual broadcast procedure, loop on peers and add the message to their queues
func (s *Neighborhood) Broadcast(msg []byte) error {

	if len(s.peers) + len(s.inpeers) == 0 {
		return errors.New("No peers in neighborhood")
	}

	s.oldMessageMu.RLock()
	if _, ok := s.oldMessageQ[string(msg)]; ok {
		// todo : - have some more metrics for termination
		// todo	: - maybe tell the peer weg ot this message already?
		s.oldMessageMu.RUnlock()
		return errors.New("old message")
	}
	s.oldMessageMu.RUnlock()

	s.oldMessageMu.Lock()
	s.oldMessageQ[string(msg)] = struct{}{}
	s.oldMessageMu.Unlock()

	s.peersMutex.RLock()
	for p := range s.peers {
		peer := s.peers[p]
		err := peer.addMessage(msg)
		if err != nil {
			// report error and maybe replace this peer
			s.Errorf("Err adding message err=", err)
			continue
		}
		s.Debug("adding message to peer %v", peer.Pretty())
	}
	s.peersMutex.RUnlock()
	s.inpeersMutex.RLock()
	for p := range s.inpeers {
		peer := s.inpeers[p]
		err := peer.addMessage(msg)
		if err != nil {
			// report error and maybe replace this peer
			s.Errorf("Err adding message err=", err)
			continue
		}
		s.Debug("adding message to peer %v", peer.Pretty())
	}
	s.inpeersMutex.RUnlock()

	//TODO: if we didn't send to RandomConnections then try to other peers.
	return nil
}

func (s *Neighborhood) getMorePeers(numpeers int) {

	if numpeers == 0 {
		return
	}

	type cnErr struct {
		n   node.Node
		c   net.Connection
		err error
	}

	res := make(chan cnErr, numpeers)

	// dht should provide us with random peers to connect to
	nds := s.ps.SelectPeers(numpeers)
	ndsLen := len(nds)
	if ndsLen == 0 {
		s.Debug("Peer sampler returned nothing.")
		// this gets busy at start so we spare a second
		time.Sleep(NoResultsTimeout)
		go s.getMorePeers(numpeers)
		return // zero samples here so no reason to proceed
	}

	// Try a connection to each peer.
	// TODO: try splitting the load and don't connect to more than X at a time
	for i := 0; i < ndsLen; i++ {
		go func(nd node.Node, reportChan chan cnErr) {
			spew.Dump(nd)
			c, err := s.cp.GetConnection(nd.Address(), nd.PublicKey())
			reportChan <- cnErr{nd, c, err}
		}(nds[i], res)
	}

	i, j := 0, 0
	tm := time.NewTimer(time.Second * 20 * time.Duration(ndsLen))
loop:
	for {
		select {
		case cne := <-res:
			i++ // We count i everytime to know when to close the channel

			if cne.err != nil && cne.n == node.EmptyNode {
				s.Error("can't establish connection with sampled peer %v, %v", cne.n.String(), cne.err)
				j++
				continue // this peer didn't work, todo: tell dht
			}

			p := makePeer(cne.n, cne.c, s.Log)
			s.inpeersMutex.Lock()
			//_, ok := s.outbound[cne.n.String()]
			_, ok := s.inpeers[cne.n.PublicKey().String()]
			if ok {
				delete(s.inpeers, cne.n.PublicKey().String())
			}
			s.inpeersMutex.Unlock()

			s.peersMutex.Lock()
			s.peers[cne.n.PublicKey().String()] = p
			s.peersMutex.Unlock()

			go p.start(s.remove)
			s.Debug("Neighborhood: Added peer to peer list %v", cne.n.Pretty())

			if i == numpeers {
				break loop
			}
		case <-tm.C:
			break loop
		}
	}

	if len(s.peers) < s.config.RandomConnections {
		s.Debug("getMorePeers enough calling again")
		s.getMorePeers(s.config.RandomConnections - len(s.peers))
		return
	}
}

// Start Neighborhood manages the peers we are connected to all the time
// It connects to config.RandomConnections and after that maintains this number
// of connections, if a connection is closed it should send a channel message that will
// trigger new connections to fill the requirement.
func (s *Neighborhood) Start() error {
	//TODO: Save and load persistent peers ?

	// initial
	s.morePeersReq <- struct{}{}
	ret := make(chan struct{})

	go func() {
		var o sync.Once
	loop:
		for {
			select {
			case torm := <-s.remove:
					go s.Disconnect(torm)
			case <-s.morePeersReq:
				if len(s.peers) >= s.config.RandomConnections {
					o.Do(func() { close(ret) })
				} else {
					go s.getMorePeers(s.config.RandomConnections - len(s.peers))
				}
			case <-s.shutdown:
				break loop // maybe error ?
			}
		}
	}()

	<-ret

	return nil
}

func (s *Neighborhood) Disconnect(peer string) {
	s.inpeersMutex.Lock()
	if _, ok := s.inpeers[peer];ok {
		delete(s.inpeers, peer)
		s.inpeersMutex.Unlock()
		return
	}
	s.inpeersMutex.Unlock()

	s.peersMutex.Lock()
	if _, ok := s.peers[peer]; ok {
		delete(s.peers, peer)
	}
	s.peersMutex.Unlock()
	s.morePeersReq <- struct{}{}
}

func (s *Neighborhood) AddPeer(n node.Node, c net.Connection) {
	p := makePeer(n, c, s.Log)
	s.inpeersMutex.Lock()
	s.inpeers[n.PublicKey().String()] = p
	s.peersMutex.Unlock()
	go p.start(s.remove)
}
