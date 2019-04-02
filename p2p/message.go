package p2p

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/pb"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"time"
)

// NewProtocolMessageMetadata creates meta-data for an outgoing protocol message authored by this node.
func NewProtocolMessageMetadata(author p2pcrypto.PublicKey, protocol string) *pb.Metadata {
	return &pb.Metadata{
		NextProtocol:  protocol,
		ClientVersion: config.ClientVersion,
		Timestamp:     time.Now().Unix(),
		AuthPubkey:    author.Bytes(),
	}
}

func ExtractData(pm *pb.ProtocolMessage) (service.Data, error) {
	var data service.Data
	if payload := pm.GetPayload(); payload != nil {
		data = &service.DataBytes{Payload: payload}
	} else if wrap := pm.GetMsg(); wrap != nil {
		data = &service.DataMsgWrapper{Req: wrap.Req, MsgType: wrap.Type, ReqID: wrap.ReqID, Payload: wrap.Payload}
	} else {
		return nil, errors.New("not valid data type")
	}
	return data, nil
}

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
	data           service.Data
	validationChan chan service.MessageValidation
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

func (pm gossipProtocolMessage) ReportValidation(protocol string, isValid bool) {
	if pm.validationChan != nil {
		pm.validationChan <- service.NewMessageValidation(pm.Bytes(), protocol, isValid)
	}
}
