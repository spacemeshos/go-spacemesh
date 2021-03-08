package keepalive

import (
	"bytes"
	"errors"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
)

//Name is the name of the protocol
const Name = "tcp/v1/keepalive"

//MessageBufSize is the buffer size for the messages channel
const MessageBufSize = 1000

// KeepAlivePing is the ping protocol ID
const KeepAlivePing = 0

// DurationBetweenPings determines how long we need to wait between ping messages
const DurationBetweenPings = time.Second * 10

type baseNetwork interface {
	RegisterDirectProtocolWithChannel(protocol string, ingressChannel chan service.DirectMessage) chan service.DirectMessage
	SendWrappedMessage(nodeID p2pcrypto.PublicKey, protocol string, payload *service.DataMsgWrapper) error
	SubscribePeerEvents() (conn, disc chan p2pcrypto.PublicKey)
	Disconnect(peer p2pcrypto.PublicKey)
}

type protocol struct {
	local                 *node.Info
	connectPeerChannel    chan p2pcrypto.PublicKey
	disconnectPeerChannel chan p2pcrypto.PublicKey
	msgServer             *server.MessageServer
	logger                log.Log
	peers                 map[p2pcrypto.PublicKey]struct{}
	net                   baseNetwork
}

func newProtocol(peerChan chan p2pcrypto.PublicKey, svc baseNetwork, log log.Log,
	messageTimeout time.Duration) *protocol {
	s := server.NewMsgServer(svc, Name, messageTimeout, make(chan service.DirectMessage, MessageBufSize), log)
	conn, disc := svc.SubscribePeerEvents()
	p := &protocol{
		connectPeerChannel:    conn,
		disconnectPeerChannel: disc,
		msgServer:             s,
	}
	p.msgServer.RegisterMsgHandler(KeepAlivePing, p.handleKeepAlivePingRequest())

	//spin up a thread that loops on the input from the two channels
	return p
}

func (p *protocol) handleKeepAlivePingRequest() func(msg server.Message) []byte {
	return func(msg server.Message) []byte {
		plogger := p.logger.WithFields(log.String("type", "keepalive-ping"), log.String("from", msg.Sender().String()))
		plogger.Debug("Handle keep-alive request")
		//what should the contents of this message be?
		pinged := &node.Info{}
		err := types.BytesToInterface(msg.Bytes(), pinged)
		if err != nil {
			plogger.Error("failed to deserialize ping message err=", err)
			return nil
		}

		payload, err := types.InterfaceToBytes(p.local)
		if err != nil {
			plogger.Error("Error marshalling response for keepalive ping")
			return nil
		}
		plogger.Debug("Sending response to keepalive ping")
		return payload
	}
}

// this function will be repeatedly called in a loop to send pings to each of the neighbors
func (p *protocol) handleKeepAlivePing(peer p2pcrypto.PublicKey) error {
	plogger := p.logger.WithFields(log.String("type", "keepalive-ping"), log.String("to", peer.String()))
	plogger.Debug("Send keepalive ping request")

	data, err := types.InterfaceToBytes(p.local)
	if err != nil {
		return err
	}
	ch := make(chan []byte)
	foo := func(msg []byte) {
		defer close(ch)
		plogger.Debug("Handle response to keepalive ping")
		sender := &node.Info{}
		err := types.BytesToInterface(msg, sender)

		if err != nil {
			plogger.Warning("got unreadable response, err=%v", err)
			return
		}

		ch <- sender.ID.Bytes()
	}

	err = p.msgServer.SendRequest(KeepAlivePing, data, peer, foo, func(err error) {})

	if err != nil {
		return err
	}

	timeout := time.NewTimer(time.Second * 100)
	select {
	case id := <-ch:
		if id == nil {
			return errors.New("failed sending message")
		}
		if !bytes.Equal(id, peer.Bytes()) {
			return errors.New("got response with a different public key")
		}
	case <-timeout.C:
		p.net.Disconnect(peer)
		return errors.New("timed out, there need to remove the peer")
	}

	return nil
}

func (p *protocol) pingLoop() {
	for {
		//send ping
		p.handleKeepAlivePing()
		time.Sleep(DurationBetweenPings)
	}
}
