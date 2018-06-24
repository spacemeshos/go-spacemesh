package p2p

import (
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"errors"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/dht"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/pb"
	"github.com/spacemeshos/go-spacemesh/crypto"
)

const (
	// BootstrapTimeout is the maximum time we allow bootstrap phase to run
	BootstrapTimeout = 3 * time.Minute
	// ConnectToNodeTimeout is the timeout we allow for a connection to a single random node.
	ConnectToNodeTimeout = 30 * time.Second
)

func (s *swarmImpl) bootstrap() {
	s.localNode.Info("Starting bootstrap...")

	c := int(s.config.SwarmConfig.RandomConnections)
	if c <= 0 {
		s.localNode.Error("0 random connections - aborting bootstrap")
		return
	}

	// register bootstrap nodes
	bn := uint32(0)
	for _, n := range s.config.SwarmConfig.BootstrapNodes {
		//connected := make(chan error)
		//go func(q chan error) {
		//	c := <-q
		//	if c == nil {
		//		atomic.AddUint32(&bn, 1)
		//	}
		//}(connected)
		rn, err := node.NewNodeFromString(n)

		if err != nil {
			s.localNode.Errorf("Could'nt parse node from string, skipping (err:  %v)", err)
			continue
		}

		if s.localNode.String() != rn.String() {
			s.onRegisterNodeRequest(rn)
			//s.onConnectionRequest(rn, connected)
			remotePub, e := crypto.NewPublicKey(rn.Bytes())
			if e != nil {
				// TODO handle error (not really, @Yosher promised that req will have a crypto.PublicKey)
			}
			_, e = s.cPool.getConnection(rn.IP(), remotePub)
			if (e != nil) {
				s.localNode.Warning("failed to connect to bootstrap node %v. err: %v", rn.ID(), e)
				break
			}
			atomic.AddUint32(&bn, 1)
		}
	}

	// from now on everything will be run in a goroutine to allow the bootstrap event loop to work with protocols.
	// issue findNode requests until DHT has at least c peers and start a periodic refresh func
	go func() {
		timeout := time.NewTimer(BootstrapTimeout)
		for {
			select {
			case <-timeout.C:
				s.localNode.Error("Failed to bootstrap node")
				s.bootComplete(errors.New("failed to bootstrap node"))
				return
			default:
				if atomic.LoadUint32(&bn) > 0 {
					errSignal := make(chan error)
					go s.routingTable.Bootstrap(s.kadFindNode, s.localNode.String(), c, errSignal)

					err := <-errSignal
					if err != nil {
						s.localNode.Error("Bootstrapping the node failed ", err)
						return
					}
					s.localNode.Debug("Bootstrap filled routing table, establishing k random connections")
					if s.routingTable.IsHealthy() {
						//s.ConnectToRandomNodes(c)
						s.bootComplete(nil)
					}
					return
				}
			}
		}
	}()
}

// Connect up to count random nodes
func (s *swarmImpl) ConnectToRandomNodes(count int) {

	s.localNode.Info("Attempting to connect to %d random nodes...", count)

	// create callback to receive result
	c1 := make(dht.PeersOpChannel)

	// find nearest peers
	s.routingTable.NearestPeers(dht.NearestPeersReq{ID: s.localNode.DhtID(), Count: count, Callback: c1})

	c := <-c1
	if len(c.Peers) == 0 {
		s.LocalNode().Warning("Did not find any random nodes close to self")
	}

	wg := sync.WaitGroup{}

	for _, p := range c.Peers {
		wg.Add(1)
		done := make(chan error)
		go func(d chan error, p node.Node) {
			go func() {
				remotePub, err := crypto.NewPublicKey(p.Bytes())
				if err != nil {
					// TODO handle error (not really, @Yosher promised that req will have a crypto.PublicKey)
				}
				_, err = s.cPool.getConnection(p.IP(), remotePub)
				d <- err
			}()
			timeout := time.NewTimer(ConnectToNodeTimeout)
			select {
			case derr := <-d:
				if derr != nil {
					s.localNode.Error("Failed to connect with node %v, err: %v", p.Pretty(), derr)
					// TODO : retry connecting
				}
				wg.Done()
			case <-timeout.C:
				s.localNode.Error("Failed to log connect node %v", p.Pretty())
				wg.Done()
			}
		}(done, p)
	}

	wg.Wait()

}

func (s *swarmImpl) onSendInternalMessage(r SendMessageReq) {

	s.peerMapMutex.RLock()
	peer := s.peers[r.PeerID]
	s.peerMapMutex.RUnlock()

	for peer == nil {
		prs := s.getNearestPeers(node.NewDhtIDFromBase58(r.PeerID), 1)
		if len(prs) == 0 {
			//We're not sending to peers the routing table don't know about in bootstrap phase
			return
		}
		p := prs[0]
		s.onRegisterNodeRequest(p)
	}

	conn, err := s.cPool.getConnection(peer.TCPAddress(), peer.PublicKey())//peer.GetActiveConnection()
	if err != nil {
		s.localNode.Warning("failed to send message to %v, no valid connection. err: %v", peer.ID(), err)
		return
	}
	session := conn.Session()
	if session == nil {
		s.localNode.Warning("failed to send message to %v, no valid session. err: %v", peer.ID(), err)
		return
	}
	//
	//if conn == nil || session == nil {
	//	if _, exist := s.messagesPendingSession[hex.EncodeToString(r.ReqID)]; !exist {
	//		s.messagesPendingSession[hex.EncodeToString(r.ReqID)] = r
	//	}
	//	return
	//}

	encPayload, err := session.Encrypt(r.Payload)
	if err != nil {
		e := fmt.Errorf("aborting send - failed to encrypt payload: %v", err)
		go func() {
			if r.Callback != nil {
				r.Callback <- SendError{r.ReqID, e}
			}
		}()
		return
	}

	msg := &pb.CommonMessageData{
		SessionId: session.ID(),
		Payload:   encPayload,
		Timestamp: time.Now().Unix(),
	}

	data, err := proto.Marshal(msg)
	if err != nil {
		e := fmt.Errorf("aborting send - invalid msg format %v", err)
		go func() {
			if r.Callback != nil {
				r.Callback <- SendError{r.ReqID, e}
			}
		}()
		return
	}

	// store callback by reqId for this connection so we can call back in case of msg timeout or other send failure
	if r.Callback != nil {
		callbacks := s.outgoingSendsCallbacks[conn.ID()]
		if callbacks == nil {
			s.outgoingSendsCallbacks[conn.ID()] = make(map[string]chan SendError)
		}

		s.outgoingSendsCallbacks[conn.ID()][hex.EncodeToString(r.ReqID)] = r.Callback
	}

	s.localNode.Debug("Sending message to %v", log.PrettyID(r.PeerID))

	conn.Send(data, r.ReqID)
}

func (s *swarmImpl) bootstrapLoop(retryTicker *time.Ticker) {
	s.localNode.GetLogger().Info("DEBUG: swarm::bootstrapLoop")
	s.bootstrap()
BSLOOP:
	for {
		select {
		case <-s.shutdown:
			s.shutDownInternal()
			return
		case <-s.bootstrapped:
			break BSLOOP
		//case r := <-s.sendHandshakeMsg:
		//	s.onSendHandshakeMessage(r)
		//case session := <-s.newSessions:
		//	s.onNewSession(session)
		case m := <-s.network.GetIncomingMessage():
			s.onRemoteClientMessage(m)
		//case n := <-s.connectionRequests:
		//	s.onConnectionRequest(n.req, n.done)
		case r := <-s.sendMsgRequests:
			go s.onSendInternalMessage(r)
		case qmsg := <-s.retryMessage:
			s.retryMessageLater(qmsg)
		case <-retryTicker.C:
			s.resendRetryMessages()
		}
	}
}
