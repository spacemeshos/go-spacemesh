package p2p2

import (
	"github.com/UnrulyOS/go-unruly/log"
	"net"
	"time"
)

// Connection manager able to dial remote endpoints
// To use this manager client  should register all callbacks
// Connections may be initiated by Dial() or by remote clients connecting to the listen address
// ConnManager includes a TCP server, and a TCP client
// It provides full duplex messaging functionality over the same tcp/ip connection
type Network interface {

	DialTCP(address string, timeOut time.Duration) (Connection, error)	   // Connect to a remote node

	OnRemoteClientConnected(callBack func(c Connection))               // remote tcp client connected to us
	OnConnectionClosed(callBack func(c Connection))                    // a connection is closing
	OnRemoteClientMessage(callBack func(c Connection, message []byte)) // new remote tcp client message
	OnConnectionError(callBack func(c Connection, err error))          // connection error
}

// impl internal tpye
type network struct {
	tcpListener              net.Listener
	tcpListenAddress         string // Address to open connection: localhost:9999

	remoteClientConnected func(c Connection)
	connectionClosed      func(c Connection)
	remoteClientMessage   func(c Connection, message []byte)
	connectionError       func(c Connection, err error)
}

// Creates a new network
// Attempts to tcp listen on address. e.g. localhost:1234
func NewNetwork(tcpListenAddress string) (Network, error) {
	log.Info("Creating server with tcp address: %s", tcpListenAddress)
	n := &network{
		tcpListenAddress: tcpListenAddress,
	}

	err := n.listen()

	if err != nil {
		return nil, err
	}

	// set empty callbacks to avoid panics
	n.remoteClientConnected = func(c Connection) {}
	n.connectionClosed = func(c Connection) {}
	n.remoteClientMessage = func(c Connection, message []byte) {}
	n.connectionError = func(c Connection, err error) {}

	return n, nil
}

func (cm *network) OnRemoteClientConnected(callBack func(c Connection)) {
	cm.remoteClientConnected = callBack
}

func (cm *network) OnConnectionClosed(callBack func(c Connection)) {
	cm.connectionClosed = callBack
}

func (cm *network) OnRemoteClientMessage(callBack func(c Connection, message []byte)) {
	cm.remoteClientMessage = callBack
}

func (cm *network) OnConnectionError(callBack func(c Connection, err error)) {
	cm.connectionError = callBack
}

// Dial a remote server with provided time out
// address:: ip:port
// Returns established connection or error
func (cm *network) DialTCP(address string, timeOut time.Duration) (Connection, error) {

	// connect via dialer so we can set its params
	dialer := &net.Dialer{}
	dialer.KeepAlive = time.Duration(48 * time.Hour)	// drop connections after a period of inactivity
	dialer.Timeout = time.Duration(1 * time.Minute)

	netConn, err := dialer.Dial("tcp", address)

	if err != nil {
		log.Error("Failed to tcp connect to: %s", address)
		return nil, err
	}

	c := newConnection(netConn, cm)
	return c, nil
}

// Start network server
func (cm *network) listen() error {
	listener, err := net.Listen("tcp", cm.tcpListenAddress)
	if err != nil {
		log.Error("Error starting TCP server: %v", err)
		return err
	}
	defer listener.Close()
	go cm.acceptTcp()
	return nil
}

func (cm *network) acceptTcp() {
	for {
		netConn, err := cm.tcpListener.Accept()
		if err != nil {
			log.Warning("Failed to accept connection request: %v", err)
			return
		}

		c := newConnection(netConn, cm)
		cm.remoteClientConnected(c)
	}
}
