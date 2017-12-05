package tests

import (
	"github.com/UnrulyOS/go-unruly/assert"
	"github.com/UnrulyOS/go-unruly/crypto"
	"testing"

	libp2pcrypto "gx/ipfs/QmaPbCnUMBohSGo3KnxEa2bHqyJVVeEEcwtqJAYxerieBo/go-libp2p-crypto"
)

func testIds(t *testing.T) {

	_, pub, _ := crypto.GenerateKeyPair(libp2pcrypto.Secp256k1, 256)
	id, _ := pub.IdFromPubKey()

	idStr := id.String()

	id1, err := crypto.NewId(idStr)
	if err != nil {
		t.Fatalf("failed to create id from id str: %v", err)
	}

	assert.Equal(t, id.String(), id1.String(), "expected same id")
	assert.Equal(t, string(id.Bytes()), string(id1.Bytes()), "expected same id")

}
