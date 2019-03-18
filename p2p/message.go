package p2p

import (
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

//type p2pMetadata struct {
//	data map[string]string // connection address
//	// todo: anything more ?
//}
//
//func (m p2pMetadata) set(k, v string) {
//	m.data[k] = v
//}
//
//// Address is the address on which a connection
//func (m p2pMetadata) Get(k string) (string, bool) {
//	v, ok := m.data[k]
//	return v, ok
//}
//
//func newP2PMetadata() p2pMetadata {
//	return p2pMetadata{make(map[string]string)}
//}

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
