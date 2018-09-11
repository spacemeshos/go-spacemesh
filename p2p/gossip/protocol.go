package gossip

import (
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/message"
	"github.com/spacemeshos/go-spacemesh/p2p/net"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"sync"
	"time"
)

const PeerMessageQueueSize = 100

type Protocol interface {
	Broadcast(payload []byte) error
	Start() error
	Peer(pubkey string) (node.Node, net.Connection)
	Shutdown()
}

type PeerSampler interface {
	SelectPeers(count int) []node.Node
}

type ConnectionFactory interface {
	GetConnection(address string, pk crypto.PublicKey) (net.Connection, error)
}

type Neighborhood struct {
	log.Log

	config config.SwarmConfig

	peers        map[string]*peer
	morePeersReq chan struct{}

	oldMessageMu sync.RWMutex
	oldMessageQ  map[string]struct{}

	ps PeerSampler

	cp ConnectionFactory

	shutdown chan struct{}

	peersMutex sync.RWMutex
}

func NewNeighborhood(config config.SwarmConfig, ps PeerSampler, cp ConnectionFactory, log2 log.Log) *Neighborhood {
	return &Neighborhood{
		Log:          log2,
		config:       config,
		morePeersReq: make(chan struct{}, config.RandomConnections),
		peers:        make(map[string]*peer, config.RandomConnections),
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
		make(chan error),
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

func (p *peer) start(dischann chan struct{}) {
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
				return
			}
		case d := <-p.disc:
			log.Error("peer disconnected %v", d)
			p.conn.Close()
			if dischann != nil {
				dischann <- struct{}{}
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

	s.oldMessageMu.RLock()
	if _, ok := s.oldMessageQ[string(msg)]; ok {
		// todo : - have some more metrics for termination
		// todo	: - maybe tell the peer weg ot this message already?
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

	//TODO: if we didn't send to RandomConnections then try to other peers.
	return nil
}

func (s *Neighborhood) getMorePeers(numpeers int) {
	type cnErr struct {
		n   node.Node
		c   net.Connection
		err error
	}

	res := make(chan cnErr, numpeers)

	// dht should provide us with random peers to connect to
	nds := s.ps.SelectPeers(numpeers)
	if len(nds) == 0 {
		return // we cant connect if we don't have peers
	}

	// Try a connection to each peer.
	// TODO: try splitting the load and don't connect to more than X at a time
	for i := 0; i < numpeers; i++ {
		go func(nd node.Node, reportChan chan cnErr) {
			c, err := s.cp.GetConnection(nd.Address(), nd.PublicKey())
			reportChan <- cnErr{nd, c, err}
		}(nds[i], res)
	}

	i, j := 0, 0
	for cne := range res {
		i++ // We count i everytime to know when to close the channel

		if cne.err != nil {
			j++
			s.morePeersReq <- struct{}{}
			continue // this peer didn't work, todo: tell dht
		}
		s.peersMutex.RLock()
		_, ok := s.peers[cne.n.String()]
		s.peersMutex.RUnlock()
		if ok { // peer exists already
			j++
			s.morePeersReq <- struct{}{}
			continue
		}
		peer := makePeer(cne.n, cne.c, s.Log)
		s.peersMutex.Lock()
		s.peers[cne.n.String()] = peer
		s.peersMutex.Unlock()
		s.Debug("Neighborhood: Added peer to peer list %v", cne.n.Pretty())
		go peer.start(s.morePeersReq)

		if i == numpeers {
			close(res)
		}
	}
	s.Info("Connected to %d/%d peers", i-j, s.config.RandomConnections)

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
	var o sync.Once

	go func() {

	loop:
		for {
			select {
			case <-s.morePeersReq:
				pl := len(s.peers)
				num := s.config.RandomConnections - pl
				if num > 0 {
					s.Info("%d/%d peers connected, getting %v more ", pl, s.config.RandomConnections, num)
					s.getMorePeers(num)
				}

				if len(s.peers) == s.config.RandomConnections {
					o.Do(func() { ret <- struct{}{} })
				}
			case <-s.shutdown:
				break loop // maybe error ?
			}
		}
	}()

	<-ret
	return nil
}
