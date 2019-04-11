package pb

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"time"
)

// NewProtocolMessageMetadata creates meta-data for an outgoing protocol message authored by this node.
func NewProtocolMessageMetadata(author p2pcrypto.PublicKey, protocol string) *Metadata {
	return &Metadata{
		NextProtocol:  protocol,
		ClientVersion: config.ClientVersion,
		Timestamp:     time.Now().Unix(),
		AuthPubkey:    author.Bytes(),
	}
}

// NewProtocolMessageMetadata creates meta-data for an outgoing protocol message authored by this node.
func NewUDPProtocolMessageMetadata(author p2pcrypto.PublicKey, networkID int8, protocol string) *UDPMetadata {
	return &UDPMetadata{
		NextProtocol:  protocol,
		ClientVersion: config.ClientVersion,
		NetworkID:     int32(networkID),
		Timestamp:     time.Now().Unix(),
	}
}

func CreatePayload(data service.Data) (*Payload, error) {
	switch x := data.(type) {
	case service.DataBytes:
		return &Payload{Data: &Payload_Payload{x.Bytes()}}, nil
	case *service.DataMsgWrapper:
		return &Payload{Data: &Payload_Msg{&MessageWrapper{Type: x.MsgType, Req: x.Req, ReqID: x.ReqID, Payload: x.Payload}}}, nil
	case nil:
		// The field is not set.
	default:
		return nil, fmt.Errorf("protocolMsg has unexpected type %T", x)
	}

	return nil, errors.New("input was nil")
}

func ExtractData(pm *Payload) (service.Data, error) {
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
