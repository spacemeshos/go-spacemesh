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

// NoResultsInterval is the timeout we wait between requesting more peers repeatedly
const NoResultsInterval = 1 * time.Second

// PeerMessageQueueSize is the len of the msgQ each peer holds.
const PeerMessageQueueSize = 100

// ConnectingTimeout is the timeout we wait when trying to connect a neighborhood
const ConnectingTimeout = 20 * time.Second //todo: add to the config

// fnv.New32 must be used everytime to be sure we get consistent results.
func generateID(msg []byte) uint32 {
	msghash := fnv.New32() // todo: Add nonce to messages instead
	msghash.Write(msg)
	return msghash.Sum32()
}

// Protocol is a simple declaration of the gossip protocol
type Protocol interface {
	Broadcast(payload []byte) error

	Start() error
	Close()

	Initial() <-chan struct{}
	AddIncomingPeer(node.Node, net.Connection)
	Disconnect(key crypto.PublicKey)
}

// PeerSampler is a our interface to select peers
type PeerSampler interface {
	SelectPeers(count int) []node.Node
}

// ConnectionFactory is our interface to get connections
type ConnectionFactory interface {
	GetConnection(address string, pk crypto.PublicKey) (net.Connection, error)
}

// nodeConPair
type nodeConPair struct {
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

	morePeersReq      chan struct{}
	connectingTimeout time.Duration

	inc chan nodeConPair // report incoming connection

	// we make sure we don't send a message twice
	oldMessageMu sync.RWMutex
	oldMessageQ  map[uint32]struct{}

	ps PeerSampler

	cp ConnectionFactory

	shutdown chan struct{}

	peersMutex   sync.RWMutex
	inpeersMutex sync.RWMutex
}

// NewNeighborhood creates a new gossip protocol from type Neighborhood.
func NewNeighborhood(config config.SwarmConfig, ps PeerSampler, cp ConnectionFactory, log2 log.Log) *Neighborhood {
	return &Neighborhood{
		Log:               log2,
		config:            config,
		initial:           make(chan struct{}),
		morePeersReq:      make(chan struct{}, config.RandomConnections),
		connectingTimeout: ConnectingTimeout,
		peers:             make(map[string]*peer, config.RandomConnections),
		inpeers:           make(map[string]*peer, config.RandomConnections),
		oldMessageQ:       make(map[uint32]struct{}), // todo : remember to drain this
		shutdown:          make(chan struct{}),
		ps:                ps,
		cp:                cp,
	}
}

// Make sure that neighborhood works as a Protocol in compile time
var _ Protocol = new(Neighborhood)

type peer struct {
	log.Log
	node.Node
	connected     time.Time
	conn          net.Connection
	knownMessages map[uint32]struct{}
	msgQ          chan []byte
}

func makePeer(node2 node.Node, c net.Connection, log log.Log) *peer {
	return &peer{
		log,
		node2,
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
func (p *peer) addMessage(msg []byte, checksum uint32) error {
	// dont do anything if this peer know this msg

	if _, ok := p.knownMessages[checksum]; ok {
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
		p.knownMessages[checksum] = struct{}{}
	default:
		return errors.New("Q was full")

	}

	return nil
}

func (p *peer) start() {
	for {
		m := <-p.msgQ
		err := p.send(m)
		if err != nil {
			// todo: stop only when error is critical (identify errors)
			log.Error("Failed sending message to this peer %v", p.Node.PublicKey().String())
			return
		}
	}

}

// Close closes the neighborhood and shutsdown requests for more peers
func (s *Neighborhood) Close() {
	// no need to shutdown con, conpool will do so in a shutdown. the morepeerreq won't work
	close(s.shutdown)
}

// Broadcast is the actual broadcast procedure, loop on peers and add the message to their queues
func (s *Neighborhood) Broadcast(msg []byte) error {

	if len(s.peers)+len(s.inpeers) == 0 {
		return errors.New("No peers in neighborhood")
	}

	oldmessage := false

	checksum := generateID(msg)

	s.oldMessageMu.RLock()
	if _, ok := s.oldMessageQ[checksum]; ok {
		// todo : - have some more metrics for termination
		// todo	: - maybe tell the peer weg ot this message already?
		oldmessage = true
	}
	s.oldMessageMu.RUnlock()

	if !oldmessage {
		s.oldMessageMu.Lock()
		s.oldMessageQ[checksum] = struct{}{}
		s.oldMessageMu.Unlock()
	}

	s.peersMutex.RLock()
	for p := range s.peers {
		peer := s.peers[p]
		go peer.addMessage(msg, checksum)
		s.Debug("adding message to peer %v", peer.Pretty())
	}
	s.peersMutex.RUnlock()
	s.inpeersMutex.RLock()
	for p := range s.inpeers {
		peer := s.inpeers[p]
		go peer.addMessage(msg, checksum)
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
func (s *Neighborhood) getMorePeers(numpeers int) int {

	if numpeers == 0 {
		return 0
	}

	// dht should provide us with random peers to connect to
	nds := s.ps.SelectPeers(numpeers)
	ndsLen := len(nds)
	if ndsLen == 0 {
		s.Debug("Peer sampler returned nothing.")
		// this gets busy at start so we spare a second
		return 0 // zero samples here so no reason to proceed
	}

	type cnErr struct {
		n   node.Node
		c   net.Connection
		err error
	}

	aborted := make(chan struct{})
	res := make(chan cnErr, numpeers)

	// Try a connection to each peer.
	// TODO: try splitting the load and don't connect to more than X at a time
	for i := 0; i < ndsLen; i++ {
		go func(nd node.Node, reportChan chan cnErr) {
			c, err := s.cp.GetConnection(nd.Address(), nd.PublicKey())
			reportChan <- cnErr{nd, c, err}
		}(nds[i], res)
	}

	total, bad := 0, 0
	tm := time.NewTimer(s.connectingTimeout) // todo: configure
loop:
	for {
		select {
		case cne := <-res:
			total++ // We count i everytime to know when to close the channel

			if cne.err != nil || cne.n == node.EmptyNode {
				s.Error("can't establish connection with sampled peer %v, %v", cne.n.String(), cne.err)
				bad++
				if total == ndsLen {
					break loop
				}
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

			go p.start()
			s.Debug("Neighborhood: Added peer to peer list %v", cne.n.Pretty())

			if total == ndsLen {
				break loop
			}
		case <-tm.C:
			close(aborted)
			break loop
		}
	}

	return total - bad
}

// Start a loop that manages the peers we are connected to all the time
// It connects to config.RandomConnections and after that maintains this number
// of connections, if a connection is closed we notify the loop that we need more peers now.
func (s *Neighborhood) Start() error {
	//TODO: Save and load persistent peers ?
	s.Info("Neighborhood service started")

	// initial request for peers
	go s.loop()
	s.morePeersReq <- struct{}{}

	return nil
}

func (s *Neighborhood) loop() {
loop:
	for {
		select {
		case <-s.morePeersReq:
			s.Debug("loop: got morePeersReq")
			go s.askForMorePeers()
		case <-s.shutdown:
			break loop // maybe error ?
		}
	}
}

func (s *Neighborhood) askForMorePeers() {
	numpeers := len(s.peers)
	req := s.config.RandomConnections - numpeers

	if req > 0 {
		success := s.getMorePeers(req)

		if success == 0 {
			// if we could'nt get any maybe were initializing
			// wait a little bit before trying again
			time.Sleep(NoResultsInterval)
			s.morePeersReq <- struct{}{}
			return
		}

		// todo: better way then going in this everytime ?
		if len(s.peers) >= s.config.RandomConnections {
			s.initOnce.Do(func() {
				s.Info("gossip; connected to initial required neighbors - %v", len(s.peers))
				close(s.initial)
				s.peersMutex.RLock()
				s.Debug(spew.Sdump(s.peers))
				s.peersMutex.RUnlock()
			})
		}

	}
}

// Initial returns when the neighborhood was initialized.
func (s *Neighborhood) Initial() <-chan struct{} {
	return s.initial
}

// Disconnect removes a peer from the neighborhood, it requests more peers if our outbound peer count is less than configured
func (s *Neighborhood) Disconnect(key crypto.PublicKey) {
	peer := key.String()

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

// AddIncomingPeer inserts a peer to the neighborhood as a remote peer.
func (s *Neighborhood) AddIncomingPeer(n node.Node, c net.Connection) {
	p := makePeer(n, c, s.Log)
	s.inpeersMutex.Lock()
	s.inpeers[n.PublicKey().String()] = p
	s.inpeersMutex.Unlock()
	go p.start()
}
