package core

import (
	"bytes"
	"crypto/ed25519"
	"fmt"

	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
)

func SignRawTx(tx []byte, genesisID types.Hash20, pk signing.PrivateKey) []byte {
	hash := HashTx(tx)
	// FIXME: Prefix TX with genesis ID for signing.
	// signedData := SigningBody(genesisID[:], hash[:])
	signedData := hash[:]

	return ed25519.Sign(ed25519.PrivateKey(pk), signedData)
}

func SignedTx(tx *Tx, genesisID types.Hash20, pk signing.PrivateKey) ([]byte, error) {
	var encodedTx bytes.Buffer
	enc := scale.NewEncoder(&encodedTx)

	if _, err := tx.EncodeScale(enc); err != nil {
		return nil, fmt.Errorf("encoding deploy TX: %w", err)
	}

	sig := SignRawTx(encodedTx.Bytes(), genesisID, pk)
	if _, err := scale.EncodeByteSlice(enc, sig); err != nil {
		return nil, fmt.Errorf("encoding TX signature: %w", err)
	}

	return encodedTx.Bytes(), nil
}
