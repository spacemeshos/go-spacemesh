package connectionpool

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/net"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	inet "net"

	"bytes"
	"errors"
	"sync"
)

type dialResult struct {
	conn net.Connection
	err  error
}

type DialFunc func(address inet.Addr, remotePublicKey p2pcrypto.PublicKey) (net.Connection, error)

// ConnectionPool stores all net.Connections and make them available to all users of net.Connection.
// There are two sources of connections -
// - Local connections that were created by local node (by calling GetConnection)
// - Remote connections that were provided by a networker impl. in a pub-sub manner
type ConnectionPool struct {
	localPub    p2pcrypto.PublicKey
	dialFunc    DialFunc
	connections map[p2pcrypto.PublicKey]net.Connection
	connMutex   sync.RWMutex
	pending     map[p2pcrypto.PublicKey][]chan dialResult
	pendMutex   sync.Mutex
	dialWait    sync.WaitGroup
	logger      log.Log
	shutdown    bool
}

// NewConnectionPool creates new ConnectionPool
func NewConnectionPool(dialFunc DialFunc, lPub p2pcrypto.PublicKey, logger log.Log) *ConnectionPool {
	cPool := &ConnectionPool{
		localPub:    lPub,
		dialFunc:    dialFunc,
		connections: make(map[p2pcrypto.PublicKey]net.Connection),
		connMutex:   sync.RWMutex{},
		pending:     make(map[p2pcrypto.PublicKey][]chan dialResult),
		pendMutex:   sync.Mutex{},
		dialWait:    sync.WaitGroup{},
		logger:      logger,
		shutdown:    false,
	}

	return cPool
}

func (cp *ConnectionPool) OnNewConnection(nce net.NewConnectionEvent) error {
	if cp.isShuttingDown() {
		return errors.New("shutting down")
	}
	return cp.handleNewConnection(nce.Conn.RemotePublicKey(), nce.Conn, net.Remote)
}

func (cp *ConnectionPool) OnClosedConnection(cwe net.ConnectionWithErr) {
	if cp.isShuttingDown() {
		return
	}
	conn := cwe.Conn
	cp.logger.With().Info("connection_closed", log.String("id", conn.String()), log.String("remote", conn.RemotePublicKey().String()), log.Err(cwe.Err))
	cp.handleClosedConnection(conn)
}

func (cp *ConnectionPool) isShuttingDown() bool {
	var isd bool
	cp.connMutex.RLock()
	isd = cp.shutdown
	cp.connMutex.RUnlock()
	return isd
}

// Shutdown gracefully shuts down the ConnectionPool:
// - Closes all open connections
// - Waits for all Dial routines to complete and unblock any routines waiting for GetConnection
func (cp *ConnectionPool) Shutdown() {
	cp.connMutex.Lock()
	if cp.shutdown {
		cp.connMutex.Unlock()
		cp.logger.Error("shutdown was already called")
		return
	}
	cp.shutdown = true
	cp.connMutex.Unlock()

	cp.dialWait.Wait()
	// we won't handle the closing connection events for these connections since we exit the loop once the teardown is done
	cp.closeConnections()
}

func (cp *ConnectionPool) closeConnections() {
	cp.connMutex.Lock()
	// there should be no new connections arriving at this point
	for i, c := range cp.connections {
		c.Close()
		delete(cp.connections, i)
	}

	cp.connMutex.Unlock()
}

func (cp *ConnectionPool) CloseConnection(key p2pcrypto.PublicKey) {
	cp.connMutex.Lock()
	if c, exist := cp.connections[key]; exist {
		c.Close()
		delete(cp.connections, key)
	}
	cp.connMutex.Unlock()
}

func (cp *ConnectionPool) handleDialResult(rPub p2pcrypto.PublicKey, result dialResult) {
	cp.pendMutex.Lock()
	for _, p := range cp.pending[rPub] {
		p <- result
	}
	delete(cp.pending, rPub)
	cp.pendMutex.Unlock()
}

func compareConnections(conn1 net.Connection, conn2 net.Connection) int {
	return bytes.Compare(conn1.Session().ID().Bytes(), conn2.Session().ID().Bytes())
}

func (cp *ConnectionPool) handleNewConnection(rPub p2pcrypto.PublicKey, newConn net.Connection, source net.ConnectionSource) error {
	cp.connMutex.Lock()
	var srcPub, dstPub string
	if source == net.Local {
		srcPub = cp.localPub.String()
		dstPub = rPub.String()
	} else {
		srcPub = rPub.String()
		dstPub = cp.localPub.String()
	}
	cp.logger.With().Info("new_connection", log.String("src", srcPub), log.String("dst", dstPub))
	// check if there isn't already same connection (possible if the second connection is a Remote connection)
	curConn, ok := cp.connections[rPub]
	if ok {
		// it is possible to get a new connection with the same peers as another existing connection, in case the two peers tried to connect to each other at the same time.
		// We need both peers to agree on which connection to keep and which one to close otherwise they might end up closing both connections (bug #195)
		res := compareConnections(curConn, newConn)
		var closeConn net.Connection
		var err error

		if res <= 0 { // newConn >= curConn
			if res == 0 { // newConn == curConn
				// TODO Is it a potential threat (session hijacking)? Should we keep the existing connection?
				cp.logger.Warning("new connection was created with same session ID as an existing connection, keeping the new connection (assuming existing connection is stale). existing session ID=%v, new session ID=%v, remote=%s", curConn.Session().ID(), newConn.Session().ID(), rPub)
			} else {
				cp.logger.Warning("connection created while connection already exists between peers, closing existing connection. existing session ID=%v, new session ID=%v, remote=%s", curConn.Session().ID(), newConn.Session().ID(), rPub)
			}
			closeConn = curConn
			cp.connections[rPub] = newConn
		} else { // newConn < curConn
			closeConn = newConn
			err = fmt.Errorf("connection created while connection already exists between peers, closing new connection. existing session ID=%v, new session ID=%v, remote=%s", curConn.Session().ID(), newConn.Session().ID(), rPub)
		}
		cp.connMutex.Unlock()
		if closeConn != nil {
			closeConn.Close()
			// todo: make sure we remove this from the neighborhood
		}

		// we don't need to update on the new connection since there were already a connection in the table and there shouldn't be any registered channel waiting for updates
		return err
	}
	cp.connections[rPub] = newConn
	cp.connMutex.Unlock()

	// update all registered channels
	res := dialResult{newConn, nil}
	cp.handleDialResult(rPub, res)
	return nil
}

func (cp *ConnectionPool) handleClosedConnection(conn net.Connection) {
	cp.logger.With().Info("connection_closed", log.String("id", conn.String()), log.String("remote", conn.RemotePublicKey().String()))
	cp.connMutex.Lock()
	rPub := conn.RemotePublicKey()
	cur, ok := cp.connections[rPub]
	// only delete if the closed connection is the same as the cached one (it is possible that the closed connection is a duplication and therefore was closed)
	if ok && cur.ID() == conn.ID() {
		delete(cp.connections, rPub)
	}
	cp.connMutex.Unlock()
}

// GetConnection fetches or creates if don't exist a connection to the address which is associated with the remote public key
func (cp *ConnectionPool) GetConnection(address inet.Addr, remotePub p2pcrypto.PublicKey) (net.Connection, error) {
	cp.connMutex.RLock()
	if cp.shutdown {
		cp.connMutex.RUnlock()
		return nil, errors.New("ConnectionPool was shut down")
	}
	// look for the connection in the pool
	conn, found := cp.connections[remotePub]
	if found {
		cp.connMutex.RUnlock()
		return conn, nil
	}
	// register for signal when connection is established - must be called under the connMutex otherwise there is a race
	// where it is possible that the connection will be established and all registered channels will be notified before
	// the current registration
	cp.pendMutex.Lock()
	_, found = cp.pending[remotePub]
	pendChan := make(chan dialResult)
	cp.pending[remotePub] = append(cp.pending[remotePub], pendChan)
	if !found {
		// No one is waiting for a connection with the remote peer, need to call Dial
		cp.dialWait.Add(1)
		go func() {
			conn, err := cp.dialFunc(address, remotePub)
			if err != nil {
				cp.handleDialResult(remotePub, dialResult{nil, err})
			} else {
				cp.handleNewConnection(remotePub, conn, net.Local)
			}
			cp.dialWait.Done()
		}()
	}
	cp.pendMutex.Unlock()
	cp.connMutex.RUnlock()
	// wait for the connection to be established, if the channel is closed (in case of dialing error) will return nil
	res := <-pendChan
	return res.conn, res.err
}

// GetConnectionIfExists checks if the connection is exists or pending
func (cp *ConnectionPool) GetConnectionIfExists(remotePub p2pcrypto.PublicKey) (net.Connection, error) {
	cp.connMutex.RLock()
	if cp.shutdown {
		cp.connMutex.RUnlock()
		return nil, errors.New("ConnectionPool was shut down")
	}
	// look for the connection in the pool
	if conn, found := cp.connections[remotePub]; found {
		cp.connMutex.RUnlock()
		return conn, nil
	}
	// register for signal when connection is established - must be called under the connMutex otherwise there is a race
	// where it is possible that the connection will be established and all registered channels will be notified before
	// the current registration
	cp.pendMutex.Lock()
	if _, found := cp.pending[remotePub]; !found {
		// No one is waiting for a connection with the remote peer
		cp.connMutex.RUnlock()
		cp.pendMutex.Unlock()
		return nil, errors.New("no connection in cpool")
	}

	pendChan := make(chan dialResult)
	cp.pending[remotePub] = append(cp.pending[remotePub], pendChan)
	cp.pendMutex.Unlock()
	cp.connMutex.RUnlock()
	// wait for the connection to be established, if the channel is closed (in case of dialing error) will return nil
	res := <-pendChan
	return res.conn, res.err
}
