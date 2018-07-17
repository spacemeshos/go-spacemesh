package connectionpool

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/p2p/net"

	"sync"
	"gopkg.in/op/go-logging.v1"
	"errors"
)

type DialError struct {
	remotePub crypto.PublicKey
	err       error
}

type DialResult struct {
	conn *net.Connection
	err  error
}

type networker interface {
	Dial(address string, remotePublicKey crypto.PublicKey, networkId int8) (*net.Connection, error) // Connect to a remote node. Can send when no error.
	SubscribeOnNewRemoteConnections() chan *net.Connection
	GetNetworkId() int8
	GetClosingConnections() chan *net.Connection
	GetLogger() *logging.Logger
}

type ConnectionPool struct {
	localPub   	crypto.PublicKey
	net        	networker
	connections map[string]*net.Connection
	connMutex   sync.RWMutex
	pending     map[string][]chan DialResult
	pendMutex	sync.Mutex
dialWait    sync.WaitGroup
	shutdown    bool


	newConn		chan *net.Connection
	teardown		chan struct{}
}

func NewConnectionPool(network networker, lPub crypto.PublicKey) *ConnectionPool {
	connC := network.SubscribeOnNewRemoteConnections()
	cPool := &ConnectionPool{
		localPub:	 lPub,
		net:			network,
		connections:	make(map[string]*net.Connection),
		connMutex:		sync.RWMutex{},
		pending:		make(map[string][]chan DialResult),
		pendMutex:		sync.Mutex{},
		dialWait:    sync.WaitGroup{},
		shutdown:    false,newConn:		connC,
		teardown:		make(chan struct{}),
	}
	go cPool.beginEventProcessing()
	return cPool
}

func (cp *ConnectionPool) Shutdown() {
	cp.connMutex.Lock()
	if cp.shutdown {
		cp.net.GetLogger().Error("shutdown was already called")
		cp.connMutex.Unlock()
		return
	}
	cp.shutdown = true
	cp.connMutex.Unlock()

	cp.dialWait.Wait()
	cp.teardown <- struct{}{}
}


func (cp *ConnectionPool) handleDialError(rPub crypto.PublicKey, err error) {
	cp.pendMutex.Lock()
	for _, p := range cp.pending[rPub.String()] {
		p <- DialResult{nil, err}
	}
	delete(cp.pending, rPub.String())
	cp.pendMutex.Unlock()
}

func (cp *ConnectionPool) handleNewConnection(rPub crypto.PublicKey, conn *net.Connection) {
	cp.connMutex.Lock()
	cp.net.GetLogger().Info("new connection %v -> %v", cp.localPub, rPub)
	// check if there isn't already same connection (possible if the second connection is a Remote connection)
	cur, ok := cp.connections[rPub.String()]
	if ok {
		if cur.Source() == net.Remote {
			cp.net.GetLogger().Info("connection created by remote node while connection already exists between peers, closing new connection. remote=%s", rPub)
		} else {
			cp.net.GetLogger().Info("connection created by local node while connection already exists between peers, closing new connection. remote=%s", rPub)
		}
		conn.Close()
		cp.connMutex.Unlock()
		return
	}
	cp.connections[rPub.String()] = conn
	cp.connMutex.Unlock()

	// update all registered channels
	cp.pendMutex.Lock()
	res := DialResult{conn, nil}
	for _, p := range cp.pending[rPub.String()] {
		p <- res
	}
	delete(cp.pending, rPub.String())
	cp.pendMutex.Unlock()
}

func (cp *ConnectionPool) handleClosedConnection(conn *net.Connection) {
	cp.net.GetLogger().Info("connection %s was closed", conn.String())
	cp.connMutex.Lock()
	rPub := conn.RemotePublicKey().String()
	cur, ok := cp.connections[rPub]
	// only delete if the closed connection is the same as the cached one (it is possible that the closed connection is a duplication and therefore was closed)
	if ok && cur.ID() == conn.ID() {
		delete(cp.connections, rPub)
	}
	cp.connMutex.Unlock()
}

// fetch or create connection to the address which is associated with the remote public key
func (cp *ConnectionPool) getConnection(address string, remotePub crypto.PublicKey) (*net.Connection, error) {
	cp.connMutex.RLock()
	if cp.shutdown {
		cp.connMutex.RUnlock()
		return nil, errors.New("ConnectionPool was shut down")
	}
	// look for the connection in the pool
	conn, found := cp.connections[remotePub.String()]
	if found {
		cp.connMutex.RUnlock()
		return conn, nil
	}
	// register for signal when connection is established - must be called under the connMutex otherwise there is a race
	// where it is possible that the connection will be established and all registered channels will be notified before
	// the current registration
	cp.pendMutex.Lock()
	_, found = cp.pending[remotePub.String()]
	if !found {
		// No one is waiting for a connection with the remote peer, need to call Dial
		go func() {
			cp.dialWait.Add(1)
			conn, err := cp.net.Dial(address, remotePub, cp.net.GetNetworkId())
			if err != nil {
				cp.handleDialError(remotePub, err)
			} else {
				cp.handleNewConnection(remotePub, conn)
			}
			cp.dialWait.Done()
		}()
	}
	pendChan := make(chan DialResult)
	cp.pending[remotePub.String()] = append(cp.pending[remotePub.String()], pendChan)
	cp.pendMutex.Unlock()
	cp.connMutex.RUnlock()

	// wait for the connection to be established, if the channel is closed (in case of dialing error) will return nil
	res := <-pendChan

	return res.conn, res.err
}

// only fetch the connection, returns nil if no such connection in pool
func (cp *ConnectionPool) hasConnection(remotePub crypto.PublicKey) *net.Connection {
	cp.connMutex.RLock()
	if cp.shutdown {
		cp.connMutex.RUnlock()
		return nil
	}
	// look for the connection in the pool
	conn, found := cp.connections[remotePub.String()]
	cp.connMutex.RUnlock()
	if found {
		return conn
	}
	return nil
}

func (cp *ConnectionPool) beginEventProcessing() {
Loop:
	for {
		select {
			case conn := <-cp.newConn:

				cp.handleNewConnection( conn.RemotePublicKey(), conn)

				case conn := <-cp. net.GetClosingConnections():
						cp.handleClosedConnection(
						conn)

						case <-cp.teardown:
				cp.connMutex.Lock()
				// there should be no new connections arriving at this point
				for _, c := range cp.connections {
					// we won't handle the closing connection events for these connections since we exit the loop once the teardown is done
				c.Close()
				}
				cp.connMutex.Unlock()
				break Loop
		}
	}
}
