package message

import (
	"encoding/hex"
	"fmt"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/net"
	"github.com/spacemeshos/go-spacemesh/p2p/pb"
	"time"
)

// PrepareMessage prepares a message for sending on a given session, session must be checked first
func PrepareMessage(ns net.NetworkSession, data []byte) ([]byte, error) {
	encPayload, err := ns.Encrypt(data)
	if err != nil {
		return nil, fmt.Errorf("aborting send - failed to encrypt payload: %v", err)
	}

	return encPayload, nil
}

// NewProtocolMessageMetadata creates meta-data for an outgoing protocol message authored by this node.
func NewProtocolMessageMetadata(author crypto.PublicKey, protocol string) *pb.Metadata {
	return &pb.Metadata{
		NextProtocol:  protocol,
		ClientVersion: config.ClientVersion,
		Timestamp:     time.Now().Unix(),
		AuthPubKey:    author.Bytes(),
	}
}

// SignMessage signs a message with a privatekey.
func SignMessage(pv crypto.PrivateKey, pm *pb.ProtocolMessage) error {
	data, err := proto.Marshal(pm)
	if err != nil {
		e := fmt.Errorf("invalid msg format %v", err)
		return e
	}

	sign, err := pv.Sign(data)
	if err != nil {
		return fmt.Errorf("failed to sign message err:%v", err)
	}

	pm.Metadata.MsgSign = sign

	return nil
}

// AuthAuthor authorizes that a message is signed by its claimed author
func AuthAuthor(pm *pb.ProtocolMessage) error {
	// TODO: consider getting pubkey from outside. attackar coul'd just manipulate the whole message pubkey and sign.
	if pm == nil || pm.Metadata == nil {
		fmt.Println("WTF HAPPENED !?", pm.Metadata, pm)
		//spew.Dump(*pm)
	}

	sign := pm.Metadata.MsgSign
	sPubkey := pm.Metadata.AuthPubKey

	pubkey, err := crypto.NewPublicKey(sPubkey)
	if err != nil {
		return fmt.Errorf("could'nt create public key from %v, err: %v", hex.EncodeToString(sPubkey), err)
	}

	pm.Metadata.MsgSign = nil // we have to verify the message without the sign

	bin, err := proto.Marshal(pm)

	if err != nil {
		return err
	}

	v, err := pubkey.Verify(bin, sign)

	if err != nil {
		return err
	}

	if !v {
		return fmt.Errorf("coudld'nt verify message")
	}

	pm.Metadata.MsgSign = sign // restore sign because maybe we'll send it again ( gossip )

	return nil
}

// AuthSender authorizes that a message is signed by its claimed sender
func AuthSender(pm *pb.ProtocolMessage, key crypto.PublicKey) error {
	// TODO: consider getting pubkey from outside. attackar coul'd just manipulate the whole message pubkey and sign.
	if pm == nil || pm.Metadata == nil {
		fmt.Println("WTF HAPPENED !?", pm.Metadata, pm)
		//spew.Dump(*pm)
	}

	sign := pm.Metadata.MsgSign

	pm.Metadata.MsgSign = nil // we have to verify the message without the sign

	bin, err := proto.Marshal(pm)

	if err != nil {
		return err
	}

	v, err := key.Verify(bin, sign)

	if err != nil {
		return err
	}

	if !v {
		return fmt.Errorf("coudld'nt verify message")
	}

	pm.Metadata.MsgSign = sign // restore sign because maybe we'll send it again ( gossip )

	return nil
}
