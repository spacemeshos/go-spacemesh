package pb

import (
	"encoding/hex"
	"errors"
	"github.com/UnrulyOS/go-unruly/p2p2/keys"
	"github.com/golang/protobuf/proto"
)

func (msg *ProtocolMessage) AuthenticateAuthor() error {

	authPubKey, err := keys.NewPublicKey(msg.GetMetadata().AuthPubKey)
	signature, err := hex.DecodeString(msg.GetMetadata().AuthorSign)

	msg.GetMetadata().AuthorSign = ""

	bin, err := proto.Marshal(msg)
	if err != nil {
		return err
	}

	// restore signature
	msg.GetMetadata().AuthorSign = hex.EncodeToString(signature)

	// Verify that the signed data was signed by the public key
	v, err := authPubKey.Verify(bin, signature)
	if err != nil {
		return err
	}

	if !v {
		return errors.New("invalid signature")
	}

	return nil
}
