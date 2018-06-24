package p2p

import (
	"github.com/spacemeshos/go-spacemesh/p2p/net"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"sync"
)

type DialError struct {
	remotePub	crypto.PublicKey
	err			error
}

type DialResult struct {
	conn		*net.Connection
	err			error
}

type ConnectionPool struct {
	net        	net.Net
	connections map[string]*net.Connection
	connMutex   sync.RWMutex
	pending     map[string][]chan DialResult
	pendMutex	sync.Mutex

	newConn		chan *net.Connection
	errConn		chan DialError

}

func NewConnectionPool(network net.Net) *ConnectionPool {
	connC := net.NewConnChan()
	cPool := &ConnectionPool{
		net:			network,
		connections:	make(map[string]*net.Connection),
		connMutex:		sync.RWMutex{},
		pending:		make(map[string][]chan DialResult),
		pendMutex:		sync.Mutex{},
		newConn:		connC.Chan,
		errConn:		make(chan DialError),
	}
	go cPool.beginEventProcessing()
	cPool.net.Register(connC)
	return cPool
}

func (cp *ConnectionPool) tryGetConnection(remotePub crypto.PublicKey, networkId int32) *net.Connection {
	cp.connMutex.RLock()
	// look for the connection in the pool
	conn, found := cp.connections[remotePub.String()]
	cp.connMutex.RUnlock()
	if found {
		return conn
	}
	return nil
}

func (cp *ConnectionPool) getConnection(address string, remotePub crypto.PublicKey) (*net.Connection, error) {
	cp.connMutex.RLock()
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
			_, err := cp.net.Dial(address, remotePub, cp.net.GetNetworkId())
			if err != nil {
				cp.errConn <- DialError{remotePub, err}
			}
		}()
	}
	pendChan := make(chan DialResult)
	cp.pending[remotePub.String()] = append(cp.pending[remotePub.String()], pendChan)
	cp.pendMutex.Unlock()
	cp.connMutex.RUnlock()

	// wait for the connection to be established, if the channel is closed (in case of dailing error) will return nil
	res := <-pendChan

	return res.conn, res.err
}

func (cp *ConnectionPool) hasConnection(remotePub crypto.PublicKey) *net.Connection {
	cp.connMutex.RLock()
	// look for the connection in the pool
	c, _ := cp.connections[remotePub.String()]
	cp.connMutex.RUnlock()
	return c
}

func (cp *ConnectionPool) beginEventProcessing() {
	//cp.net.GetLogger().Info("DEBUG: ConnPool::beginEventProc")
	for {
		select {
			case conn := <-cp.newConn:
				// put new connection in connection pool
				cp.connMutex.Lock()
				rPub := conn.RemotePublicKey()
				cp.net.GetLogger().Info("new connection %v -> %v", cp.net.GetLocalNode().PublicKey(), rPub)
				cur, ok := cp.connections[rPub.String()]
				if ok {
					if cur.Source() == net.Remote {
						cp.net.GetLogger().Warning("connection created by remote node while connection already exists between peers, closing new connection. remote=%s", rPub)
						conn.Close()
						cp.connMutex.Unlock()
						continue
					} else {
						cp.net.GetLogger().Warning("connection created by local node while connection already exists between peers, dropping old connection. remote=%s", rPub)
					}
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

			case err := <-cp.errConn:
				// failed to established connection, closing all registered channels
				cp.pendMutex.Lock()
				for _, p := range cp.pending[err.remotePub.String()] {
					p <- DialResult{nil, err.err}
				}
				delete(cp.pending, err.remotePub.String())
				cp.pendMutex.Unlock()
		}
	}
}