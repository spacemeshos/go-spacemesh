package pb

import (
	"errors"
	"github.com/UnrulyOS/go-unruly/p2p2/keys"
	"github.com/golang/protobuf/proto"
)

func (msg *ProtocolMessage) AuthenticateAuthor() error {

	authPubKey, err := keys.NewPublicKey(msg.GetMetadata().AuthPubKey)
	sig := msg.GetMetadata().AuthorSign
	msg.GetMetadata().AuthorSign = ""

	bin, err := proto.Marshal(msg)
	if err != nil {
		return err
	}

	// restore signature
	msg.GetMetadata().AuthorSign = sig

	// Verify that the signed data was signed by the public key
	v, err := authPubKey.VerifyString(bin, sig)
	if err != nil {
		return err
	}

	if !v {
		return errors.New("invalid signature")
	}

	return nil
}
