package p2p

import (
	"context"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/connectionpool"
	"github.com/spacemeshos/go-spacemesh/p2p/version"
	"net"
	"time"

	"github.com/spacemeshos/go-spacemesh/log"
	inet "github.com/spacemeshos/go-spacemesh/p2p/net"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
)

// Lookuper is a service used to lookup for nodes we know already
type Lookuper func(key p2pcrypto.PublicKey) (*node.Info, error)

type udpNetwork interface {
	Shutdown()
	Dial(ctx context.Context, address net.Addr, remotePublicKey p2pcrypto.PublicKey) (inet.Connection, error)
	IncomingMessages() chan inet.IncomingMessageEvent
	SubscribeOnNewRemoteConnections(f func(event inet.NewConnectionEvent))
	SubscribeClosingConnections(f func(ctx context.Context, connection inet.ConnectionWithErr))
}

// UDPMux is a server for receiving and sending udp messages through protocols.
type UDPMux struct {
	logger log.Log

	local     node.LocalNode
	networkid int8

	cpool    cPool
	lookuper Lookuper
	network  udpNetwork

	messages    map[string]chan service.DirectMessage
	shutdownCtx context.Context
}

// NewUDPMux creates a new udp protocol server
func NewUDPMux(ctx, shutdownCtx context.Context, localNode node.LocalNode, lookuper Lookuper, udpNet udpNetwork, networkid int8, logger log.Log) *UDPMux {
	cpool := connectionpool.NewConnectionPool(shutdownCtx, udpNet.Dial, localNode.PublicKey(), logger.WithName("udp_cpool"))

	um := &UDPMux{
		logger:      logger,
		local:       localNode,
		networkid:   networkid,
		lookuper:    lookuper,
		network:     udpNet,
		cpool:       cpool,
		messages:    make(map[string]chan service.DirectMessage),
		shutdownCtx: shutdownCtx,
	}

	udpNet.SubscribeOnNewRemoteConnections(func(event inet.NewConnectionEvent) {
		ctx := log.WithNewSessionID(ctx)
		if err := cpool.OnNewConnection(ctx, event); err != nil {
			um.logger.WithContext(ctx).With().Warning("error adding udp connection to cpool", log.Err(err))
		}
	})

	udpNet.SubscribeClosingConnections(func(ctx context.Context, connection inet.ConnectionWithErr) {
		cpool.OnClosedConnection(ctx, connection)
	})

	return um
}

// Start starts the UDPMux
func (mux *UDPMux) Start() error {
	go mux.listenToNetworkMessage()
	return nil
}

// Shutdown closes the server
func (mux *UDPMux) Shutdown() {
	mux.network.Shutdown()
	mux.cpool.Shutdown()
}

func (mux *UDPMux) isShuttingDown() bool {
	select {
	case <-mux.shutdownCtx.Done():
		return true
	default:
	}
	return false
}

// listens to messages from the network layer and handles them.
func (mux *UDPMux) listenToNetworkMessage() {
	msgChan := mux.network.IncomingMessages()
	for {
		select {
		case msg, ok := <-msgChan:
			if !ok {
				// closed
				return
			}
			if mux.isShuttingDown() {
				return
			}
			go func(event inet.IncomingMessageEvent) {
				err := mux.processUDPMessage(event)
				if err != nil {
					mux.logger.Error("Error handing network message err=%v", err)
					// todo: blacklist ?
				}
			}(msg)
		case <-mux.shutdownCtx.Done():
			return
		}
	}
}

// Note: for now udp is only direct.
// todo: no need to return chan, but for now stay consistent with api

// RegisterDirectProtocolWithChannel registers a protocol on a channel, should be done before `Start` was called. not thread-safe
func (mux *UDPMux) RegisterDirectProtocolWithChannel(name string, c chan service.DirectMessage) chan service.DirectMessage {
	mux.messages[name] = c
	return c
}

// ProcessDirectProtocolMessage passes a message to the protocol.
func (mux *UDPMux) ProcessDirectProtocolMessage(sender p2pcrypto.PublicKey, protocol string, data service.Data, metadata service.P2PMetadata) error {
	// route authenticated message to the registered protocol
	msgchan := mux.messages[protocol]

	if msgchan == nil {
		return errors.New("no protocol")
	}

	msgchan <- &udpProtocolMessage{metadata, sender, data}

	return nil
}

// SendWrappedMessage is a proxy method to the sendMessageImpl. it sends a wrapped message and used within MessageServer
func (mux *UDPMux) SendWrappedMessage(ctx context.Context, nodeID p2pcrypto.PublicKey, protocol string, payload *service.DataMsgWrapper) error {
	return mux.sendMessageImpl(ctx, nodeID, protocol, payload)
}

// SendMessage is a proxy method to the sendMessageImpl.
func (mux *UDPMux) SendMessage(ctx context.Context, peerPubkey p2pcrypto.PublicKey, protocol string, payload []byte) error {
	return mux.sendMessageImpl(ctx, peerPubkey, protocol, service.DataBytes{Payload: payload})
}

// sendMessageImpl finds the peer address, wraps the message as a protocol message with p2p metadata and sends it.
// it handles looking up the peer ip address and encrypting the message.
func (mux *UDPMux) sendMessageImpl(ctx context.Context, peerPubkey p2pcrypto.PublicKey, protocol string, payload service.Data) error {
	var err error
	var peer *node.Info

	peer, err = mux.lookuper(peerPubkey)

	if err != nil {
		return err
	}

	addr := &net.UDPAddr{IP: net.ParseIP(peer.IP.String()), Port: int(peer.DiscoveryPort)}

	conn, err := mux.cpool.GetConnection(ctx, addr, peer.PublicKey())

	if err != nil {
		return err
	}

	session := conn.Session()

	if session == nil {
		return ErrNoSession
	}

	mt := ProtocolMessageMetadata{protocol,
		config.ClientVersion,
		time.Now().UnixNano(),
		mux.local.PublicKey().Bytes(),
		int32(mux.networkid),
	}

	message := ProtocolMessage{
		Metadata: &mt,
	}

	message.Payload, err = CreatePayload(payload)
	if err != nil {
		return fmt.Errorf("can't create payload, err:%v", err)
	}

	data, err := types.InterfaceToBytes(&message)
	if err != nil {
		return fmt.Errorf("failed to encode signed message err: %v", err)
	}

	// TODO: node.address should have IP address, UDP and TCP PORT.
	// 		 for now assuming it's the same port for both.

	final := session.SealMessage(data)

	realfinal := p2pcrypto.PrependPubkey(final, mux.local.PublicKey())

	if err := conn.Send(ctx, realfinal); err != nil {
		return err
	}

	mux.logger.WithContext(ctx).With().Debug("sent udp message",
		log.String("protocol", protocol),
		log.String("peer_addr", peer.String()),
		log.Int("len", len(realfinal)))
	return nil
}

type udpProtocolMessage struct {
	meta   service.P2PMetadata
	sender p2pcrypto.PublicKey
	msg    service.Data
}

func (upm *udpProtocolMessage) Sender() p2pcrypto.PublicKey {
	return upm.sender
}

func (upm *udpProtocolMessage) Metadata() service.P2PMetadata {
	return upm.meta
}

func (upm *udpProtocolMessage) Bytes() []byte {
	return upm.msg.Bytes()
}

func (upm *udpProtocolMessage) Data() service.Data {
	return upm.msg
}

// processUDPMessage processes a udp message received and passes it to the protocol, it adds related p2p metadata.
func (mux *UDPMux) processUDPMessage(msg inet.IncomingMessageEvent) error {
	if msg.Message == nil || msg.Conn == nil {
		return ErrBadFormat1
	}

	// protocol messages are encrypted in payload
	// Locate the session
	session := msg.Conn.Session()

	if session == nil {
		return ErrNoSession
	}

	rawmsg, _, err := p2pcrypto.ExtractPubkey(msg.Message)

	if err != nil {
		return err
	}

	decPayload, err := session.OpenMessage(rawmsg)
	if err != nil {
		mux.logger.Warning("failed decrypting message err=%v", err)
		return ErrFailDecrypt
	}

	pm := &ProtocolMessage{}
	err = types.BytesToInterface(decPayload, pm)
	if err != nil {
		mux.logger.Error("deserialization err=", err)
		return ErrBadFormat2
	}

	if pm.Metadata.NetworkID != int32(mux.networkid) {
		// todo: tell net to blacklist the ip or sender ?
		return fmt.Errorf("wrong NetworkID, want: %v, got: %v", mux.networkid, pm.Metadata.NetworkID)
	}

	if t, err := version.CheckNodeVersion(pm.Metadata.ClientVersion, config.MinClientVersion); err != nil || !t {
		return fmt.Errorf("wrong client version want at least: %v, got: %v, err: %v", config.MinClientVersion, pm.Metadata.ClientVersion, err)
	}

	var data service.Data

	data, err = ExtractData(pm.Payload)

	if err != nil {
		return fmt.Errorf("failed extracting data from message err:%v", err)
	}

	p2pmeta := service.P2PMetadata{FromAddress: msg.Conn.RemoteAddr()}

	return mux.ProcessDirectProtocolMessage(msg.Conn.RemotePublicKey(), pm.Metadata.NextProtocol, data, p2pmeta)

}
