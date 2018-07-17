package net

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"gopkg.in/op/go-logging.v1"
	"time"
	"github.com/spacemeshos/go-spacemesh/p2p/net/wire"
)

type ReadWriteCloserMock struct {

}

func (m ReadWriteCloserMock) Read(p []byte) (n int, err error) {
	return 0, nil
}

func (m ReadWriteCloserMock) Write(p []byte) (n int, err error) {
	return 0, nil
}

func (m ReadWriteCloserMock) Close() error {
	return nil
}

type NetworkMock struct {
	dialErr 			error
	dialConn			*Connection
	dialDelayMs			int8
	regNewRemoteConn 	[]chan *Connection
	networkId			int8
	closingConn			chan *Connection
}

func NewNetworkMock() *NetworkMock{
	return &NetworkMock{
		regNewRemoteConn:	make([]chan *Connection, 0),
		closingConn:		make(chan *Connection),
	}
}

func (n *NetworkMock) SetDialResult(conn *Connection, err error) {
	n.dialConn = conn
	n.dialErr = err
}

func (n *NetworkMock) SetDialDelayMs(delay int8) {
	n.dialDelayMs = delay
}

func (n *NetworkMock) Dial(address string, remotePublicKey crypto.PublicKey, networkId int8) (*Connection, error) {
	time.Sleep(time.Duration(n.dialDelayMs) * time.Millisecond)
	return n.dialConn, n.dialErr
}

func (n *NetworkMock) SubscribeOnNewRemoteConnections() chan *Connection {
	ch := make(chan *Connection, 20)
	n.regNewRemoteConn = append(n.regNewRemoteConn, ch)
	return ch
}

func (n NetworkMock) PublishNewRemoteConnection(conn *Connection) {
	for _, ch := range n.regNewRemoteConn {
		ch <- conn
	}
}

func (n *NetworkMock) setNetworkId(id int8) {
	n.networkId = id
}

func (n *NetworkMock) GetNetworkId() int8 {
	return n.networkId
}

func (n *NetworkMock) GetClosingConnections() chan *Connection {
	return n.closingConn
}

func (n NetworkMock) PublishClosingConnection(conn *Connection) {
	go func() {
		n.closingConn <- conn
	}()
}

func HandlePreSessionIncomingMessage(c *Connection, msg wire.InMessage) error {
	return nil
}

func (n *NetworkMock) GetLogger() *logging.Logger {
	return nil
}