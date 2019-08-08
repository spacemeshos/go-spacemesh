package p2p

import (
	"errors"
	"fmt"
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
	data           service.Data
	validationChan chan service.MessageValidation
}

func (pm gossipProtocolMessage) Sender() p2pcrypto.PublicKey {
	return pm.sender // DirectSender
}

func (pm gossipProtocolMessage) Data() service.Data {
	return pm.data
}

func (pm gossipProtocolMessage) Bytes() []byte {
	return pm.data.Bytes()
}

func (pm gossipProtocolMessage) ValidationCompletedChan() chan service.MessageValidation {
	return pm.validationChan
}

func (pm gossipProtocolMessage) ReportValidation(protocol string) {
	if pm.validationChan != nil {
		pm.validationChan <- service.NewMessageValidation(pm.sender, pm.Bytes(), protocol)
	}
}

type ProtocolMessageMetadata struct {
	NextProtocol  string
	ClientVersion string
	Timestamp     int64
	AuthPubkey    []byte
	NetworkID     int32
}

type Payload struct {
	Payload []byte
	Wrapped *service.DataMsgWrapper
}

type ProtocolMessage struct {
	Metadata *ProtocolMessageMetadata
	Payload  *Payload
}

func CreatePayload(data service.Data) (*Payload, error) {
	switch x := data.(type) {
	case service.DataBytes:
		return &Payload{Payload: x.Bytes()}, nil
	case *service.DataMsgWrapper:
		return &Payload{Wrapped: x}, nil
	case nil:
		return nil, fmt.Errorf("cant send empty payload")
	default:
	}
	return nil, fmt.Errorf("cant determine paylaod type")
}

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
