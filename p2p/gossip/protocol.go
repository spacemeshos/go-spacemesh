package gossip

import (
	"errors"
	"fmt"
	"github.com/davecgh/go-spew/spew"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/message"
	"github.com/spacemeshos/go-spacemesh/p2p/net"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"hash/fnv"
	"sync"
	"time"
)

// NoResultsTimeout is the timeout we wait between requesting more peers repeatedly
const NoResultsTimeout = 1 * time.Second

// PeerMessageQueueSize is the len of the msgQ each peer holds.
const PeerMessageQueueSize = 100

// Protocol is a simple declaration of the gossip protocol
type Protocol interface {
	Broadcast(payload []byte) error
	Start() error
	Initial() <-chan struct{}
	AddPeer(node.Node, net.Connection)
	Close()
}

// PeerSampler is a our interface to select peers
type PeerSampler interface {
	SelectPeers(count int) []node.Node
}

// ConnectionFactory is our interface to get connections
type ConnectionFactory interface {
	GetConnection(address string, pk crypto.PublicKey) (net.Connection, error)
}

// NodeConPair
type NodeConPair struct {
	node.Node
	net.Connection
}

// Neighborhood is the gossip protocol, it manages a list of peers and sends them broadcasts
type Neighborhood struct {
	log.Log

	config config.SwarmConfig

	initOnce sync.Once
	initial  chan struct{}

	peers   map[string]*peer
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

	peersMutex   sync.RWMutex
	inpeersMutex sync.RWMutex
}

// NewNeighborhood creates a new gossip protocol from type Neighborhood.
func NewNeighborhood(config config.SwarmConfig, ps PeerSampler, cp ConnectionFactory, log2 log.Log) *Neighborhood {
	return &Neighborhood{
		Log:          log2,
		config:       config,
		initial:      make(chan struct{}),
		morePeersReq: make(chan struct{}, config.RandomConnections),
		peers:        make(map[string]*peer, config.RandomConnections),
		inpeers:      make(map[string]*peer, config.RandomConnections),
		oldMessageQ:  make(map[string]struct{}), // todo : remember to drain this
		ps:           ps,
		cp:           cp,
	}
}

// Make sure that neighborhood works as a Protocol in compile time
var _ Protocol = new(Neighborhood)

type peer struct {
	log.Log
	node.Node
	disc          chan error
	connected     time.Time
	conn          net.Connection
	knownMessages map[uint32]struct{}
	msgQ          chan []byte
}

func makePeer(node2 node.Node, c net.Connection, log log.Log) *peer {
	return &peer{
		log,
		node2,
		make(chan error, 1),
		time.Now(),
		c,
		make(map[uint32]struct{}),
		make(chan []byte, PeerMessageQueueSize),
	}
}

func (p *peer) send(message []byte) error {
	if p.conn == nil || p.conn.Session() == nil {
		return fmt.Errorf("the connection does not exist for this peer")
	}
	return p.conn.Send(message)
}

// addMessages adds a message to this peer's queue
func (p *peer) addMessage(msg []byte) error {
	// dont do anything if this peer know this msg
	msghash := fnv.New32()
	msghash.Write(msg)
	if _, ok := p.knownMessages[msghash.Sum32()]; ok {
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
		p.knownMessages[msghash.Sum32()] = struct{}{}
	default:
		return errors.New("Q was full")

	}

	return nil
}

func (p *peer) start(dischann chan string) {
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

// Close closes the neighborhood and shutsdown requests for more peers
func (s *Neighborhood) Close() {
	// no need to shutdown con, conpool will do so in a shutdown. the morepeerreq won't work
	close(s.shutdown)
}

// the actual broadcast procedure, loop on peers and add the message to their queues
func (s *Neighborhood) Broadcast(msg []byte) error {

	if len(s.peers)+len(s.inpeers) == 0 {
		return errors.New("No peers in neighborhood")
	}

	oldmessage := false

	s.oldMessageMu.RLock()
	if _, ok := s.oldMessageQ[string(msg)]; ok {
		// todo : - have some more metrics for termination
		// todo	: - maybe tell the peer weg ot this message already?
		oldmessage = true
	}
	s.oldMessageMu.RUnlock()

	if !oldmessage {
		s.oldMessageMu.Lock()
		s.oldMessageQ[string(msg)] = struct{}{}
		s.oldMessageMu.Unlock()
	}

	s.peersMutex.RLock()
	for p := range s.peers {
		peer := s.peers[p]
		go peer.addMessage(msg)
		s.Debug("adding message to peer %v", peer.Pretty())
	}
	s.peersMutex.RUnlock()
	s.inpeersMutex.RLock()
	for p := range s.inpeers {
		peer := s.inpeers[p]
		go peer.addMessage(msg)
		s.Debug("adding message to peer %v", peer.Pretty())
	}
	s.inpeersMutex.RUnlock()

	//TODO: if we didn't send to RandomConnections then try to other peers.
	if oldmessage {
		return errors.New("old message")
	}

	return nil
}

// getMorePeers tries to fill the `peers` slice with dialed outbound peers that we selected from the dht.
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
		s.getMorePeers(numpeers)
		return // zero samples here so no reason to proceed
	}

	// Try a connection to each peer.
	// TODO: try splitting the load and don't connect to more than X at a time
	for i := 0; i < ndsLen; i++ {
		go func(nd node.Node, reportChan chan cnErr) {
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

			if i == ndsLen {
				break loop
			}
		case <-tm.C:
			break loop
		}
	}

	if len(s.peers) < s.config.RandomConnections {
		s.Debug("getMorePeers enough calling again")
		s.getMorePeers(s.config.RandomConnections - len(s.peers))
	}

	s.initialized()
}

func (s *Neighborhood) initialized() {
	s.Info("Reached negihborhood goal ", len(s.peers))
	s.peersMutex.RLock()
	spew.Dump(s.peers)
	s.peersMutex.RUnlock()
	s.initOnce.Do(func() { close(s.initial) })
}

// Start Neighborhood manages the peers we are connected to all the time
// It connects to config.RandomConnections and after that maintains this number
// of connections, if a connection is closed it should send a channel message that will
// trigger new connections to fill the requirement.
func (s *Neighborhood) Start() error {
	//TODO: Save and load persistent peers ?
	s.Info("Neighborhood service started")
	// initial
	s.morePeersReq <- struct{}{}

	go func() {
	loop:
		for {
			select {
			case torm := <-s.remove:
				s.Warning("Peer removed ", torm)
				go s.Disconnect(torm)
			case <-s.morePeersReq:
				s.Info("Requesting more peers")
				if len(s.peers) >= s.config.RandomConnections {
					s.initialized()
				} else {
					go s.getMorePeers(s.config.RandomConnections - len(s.peers))
				}
			case <-s.shutdown:
				break loop // maybe error ?
			}
		}
	}()

	return nil
}

// Initial returns when the neighborhood was initialized
func (s *Neighborhood) Initial() <-chan struct{} {
	return s.initial
}

// Disconnect removes a peer from the neighborhood, it requests more peers if our outbound peer count is less than configured
func (s *Neighborhood) Disconnect(peer string) {
	s.inpeersMutex.Lock()
	if _, ok := s.inpeers[peer]; ok {
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

// AddPeer inserts a peer to the neighborhood as a remote peer.
func (s *Neighborhood) AddPeer(n node.Node, c net.Connection) {
	p := makePeer(n, c, s.Log)
	s.inpeersMutex.Lock()
	s.inpeers[n.PublicKey().String()] = p
	s.inpeersMutex.Unlock()
	go p.start(s.remove)
}
