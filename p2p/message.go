package p2p

import (
	"context"
	"errors"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
)

type directProtocolMessage struct {
	metadata service.P2PMetadata
	sender   p2pcrypto.PublicKey
	data     service.Data
}

func (pm directProtocolMessage) Metadata() service.P2PMetadata {
	return pm.metadata
}

func (pm directProtocolMessage) Sender() p2pcrypto.PublicKey {
	return pm.sender
}

func (pm directProtocolMessage) Data() service.Data {
	return pm.data
}

func (pm directProtocolMessage) Bytes() []byte {
	return pm.data.Bytes()
}

type gossipProtocolMessage struct {
	sender         p2pcrypto.PublicKey
	ownMessage     bool
	data           service.Data
	validationChan chan service.MessageValidation
	requestID      string
}

func (pm gossipProtocolMessage) Sender() p2pcrypto.PublicKey {
	return pm.sender // DirectSender
}

func (pm gossipProtocolMessage) IsOwnMessage() bool {
	return pm.ownMessage
}

func (pm gossipProtocolMessage) Data() service.Data {
	return pm.data
}

func (pm gossipProtocolMessage) RequestID() string {
	return pm.requestID
}

func (pm gossipProtocolMessage) Bytes() []byte {
	return pm.data.Bytes()
}

func (pm gossipProtocolMessage) ValidationCompletedChan() chan service.MessageValidation {
	return pm.validationChan
}

func (pm gossipProtocolMessage) ReportValidation(ctx context.Context, protocol string) {
	if pm.validationChan != nil {
		// TODO(dshulyak) this definitely should not be logged in message data structure
		log.AppLog.WithContext(ctx).With().Debug("reporting valid gossip message",
			log.String("protocol", protocol),
			log.String("requestId", pm.requestID),
			log.FieldNamed("sender", pm.sender),
			log.Int("validation_chan_len", len(pm.validationChan)))
		pm.validationChan <- service.NewMessageValidation(pm.sender, pm.Bytes(), protocol, pm.requestID)
	}
}

// ProtocolMessageMetadata is a general p2p message wrapper
type ProtocolMessageMetadata struct {
	NextProtocol  string `ssz-max:"4096"`
	ClientVersion string `ssz-max:"4096"`
	Timestamp     uint64
	AuthPubkey    []byte `ssz-max:"4096"`
	NetworkID     uint32
}

const (
	// MessageGossip is for gossip message
	MessageGossip = iota + 1
	// MessageDirect is for direct message
	MessageDirect
)

// Payload holds either a byte array or a wrapped req-res message.
type Payload struct {
	MessageType uint8
	Payload     []byte `ssz-max:"10240000"`
	Wrapped     *service.DataMsgWrapper
}

// ProtocolMessage is a pair of metadata and a a payload.
type ProtocolMessage struct {
	Metadata *ProtocolMessageMetadata
	Payload  *Payload
}

// CreatePayload is a helper function to format a payload for sending.
func CreatePayload(data service.Data) (*Payload, error) {
	switch x := data.(type) {
	case service.DataBytes:
		if x.Payload == nil {
			return nil, fmt.Errorf("unable to send empty payload")
		}
		return &Payload{MessageType: MessageGossip, Payload: x.Bytes()}, nil
	case *service.DataMsgWrapper:
		return &Payload{MessageType: MessageDirect, Wrapped: x}, nil
	case nil:
		return nil, fmt.Errorf("unable to send empty payload")
	default:
	}
	return nil, fmt.Errorf("unable to determine payload type")
}

// ExtractData is a helper function to extract the payload data from a message payload.
func ExtractData(pm *Payload) (service.Data, error) {
	switch pm.MessageType {
	case MessageGossip:
		return &service.DataBytes{Payload: pm.Payload}, nil
	case MessageDirect:
		wrap := pm.Wrapped
		if wrap == nil {
			return nil, errors.New("invalid direct message")
		}
		return &service.DataMsgWrapper{Req: wrap.Req, MsgType: wrap.MsgType, ReqID: wrap.ReqID, Payload: wrap.Payload}, nil
	}
	return nil, errors.New("not valid data type")

}
