package pb

import (
	"errors"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/crypto"
)

func (msg *ProtocolMessage) AuthenticateAuthor() error {

	authPubKey, err := crypto.NewPublicKey(msg.GetMetadata().AuthPubKey)
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
