package p2p

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/p2p/net"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"gopkg.in/op/go-logging.v1"

	"sync"

)

type DialError struct {
	remotePub crypto.PublicKey
	err       error
}

type DialResult struct {
	conn *net.Connection
	err  error
}

type IncomingMessage struct {
	conn *net.Connection
	msg  []byte
}

type networker interface {
	Dial(address string, remotePublicKey crypto.PublicKey, networkId int8) (*net.Connection, error) // Connect to a remote node. Can send when no error.
	SubscribeOnNewConnections() chan *net.Connection
	GetNetworkId() int8
	GetLogger() *logging.Logger
}

type ConnectionPool struct {
	localNode   *node.LocalNode
	net         networker
	connections map[string]*net.Connection
	connMutex   sync.RWMutex
	pending     map[string][]chan DialResult
	pendMutex   sync.Mutex

	msgSubscribtions []chan IncomingMessage
	msgSubMutex      sync.RWMutex

	incFanin chan IncomingMessage

	newConn chan *net.Connection
	errConn chan DialError
}

func NewConnectionPool(network networker, lNode *node.LocalNode) *ConnectionPool {
	connC := network.SubscribeOnNewConnections()
	cPool := &ConnectionPool{
		localNode: lNode,
		net:       network,

		connections: make(map[string]*net.Connection),
		connMutex:   sync.RWMutex{},

		pending:   make(map[string][]chan DialResult),
		pendMutex: sync.Mutex{},

		newConn: connC,
		errConn: make(chan DialError),

		msgSubscribtions: make([]chan IncomingMessage, 0),
	}
	go cPool.beginEventProcessing()
	return cPool
}

func (cp *ConnectionPool) SubscribeMessages() <-chan IncomingMessage {
	inc := make(chan IncomingMessage)
	cp.msgSubMutex.Lock()
	cp.msgSubscribtions = append(cp.msgSubscribtions, inc)
	cp.msgSubMutex.Unlock()
	return inc
}

func (cp *ConnectionPool) publishMessage(message IncomingMessage) {
	cp.msgSubMutex.RLock()
	for _, mss := range cp.msgSubscribtions {
		mss <- message
	}
	cp.msgSubMutex.RUnlock()
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
			cp.net.GetLogger().Info("new connection %v -> %v", cp.localNode.PublicKey(), rPub)
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

			go func(conn *net.Connection) {
				for msg := range conn.Subscribe() {
					if msg.Error() != nil {
						cp.net.GetLogger().Error("Closing connection with ", conn.RemotePublicKey().Pretty(), " due to error err:", msg.Error())
						cp.connMutex.Lock()
						delete(cp.connections, conn.RemotePublicKey().String())
						cp.connMutex.Unlock()
						return
					}
					cp.publishMessage(IncomingMessage{conn, msg.Message()})
				}
				// TODO : close connections that idle for X time
				// error or channel closed.
			}(conn)

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
