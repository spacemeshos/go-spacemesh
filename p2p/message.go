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
	NextProtocol  string
	ClientVersion string
	Timestamp     int64
	AuthPubkey    []byte
	NetworkID     int32
}

// Payload holds either a byte array or a wrapped req-res message.
type Payload struct {
	Payload []byte
	Wrapped *service.DataMsgWrapper
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
		return &Payload{Payload: x.Bytes()}, nil
	case *service.DataMsgWrapper:
		return &Payload{Wrapped: x}, nil
	case nil:
		return nil, fmt.Errorf("unable to send empty payload")
	default:
	}
	return nil, fmt.Errorf("unable to determine payload type")
}

// ExtractData is a helper function to extract the payload data from a message payload.
func ExtractData(pm *Payload) (service.Data, error) {
	var data service.Data
	if payload := pm.Payload; payload != nil {
		data = &service.DataBytes{Payload: payload}
	} else if wrap := pm.Wrapped; wrap != nil {
		data = &service.DataMsgWrapper{Req: wrap.Req, MsgType: wrap.MsgType, ReqID: wrap.ReqID, Payload: wrap.Payload}
	} else {
		return nil, errors.New("not valid data type")
	}
	return data, nil
}
