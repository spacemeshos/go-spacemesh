package message

import (
	"encoding/hex"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/pb"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func Test_NewProtocolMessageMeatadata(t *testing.T) {
	//newProtocolMessageMetadata()
	_, pk, _ := crypto.GenerateKeyPair()
	const gossip = false

	assert.NotNil(t, pk)

	meta := NewProtocolMessageMetadata(pk, "EX", gossip)

	assert.NotNil(t, meta, "should be a metadata")
	assert.Equal(t, meta.Timestamp, time.Now().Unix())
	assert.Equal(t, meta.ClientVersion, config.ClientVersion)
	assert.Equal(t, meta.AuthPubKey, pk.Bytes())
	assert.Equal(t, meta.Protocol, "EX")
	assert.Equal(t, meta.Gossip, gossip)
	assert.Equal(t, meta.AuthorSign, "")

}

func TestSwarm_AuthAuthor(t *testing.T) {
	// create a message

	priv, pub, err := crypto.GenerateKeyPair()

	assert.NoError(t, err, err)
	assert.NotNil(t, priv)
	assert.NotNil(t, pub)

	pm := &pb.ProtocolMessage{
		Metadata: NewProtocolMessageMetadata(pub, "EX", false),
		Payload:  []byte("EX"),
	}
	ppm, err := proto.Marshal(pm)
	assert.NoError(t, err, "cant marshal msg ", err)

	// sign it
	s, err := priv.Sign(ppm)
	assert.NoError(t, err, "cant sign ", err)
	ssign := hex.EncodeToString(s)

	pm.Metadata.AuthorSign = ssign

	vererr := AuthAuthor(pm)
	assert.NoError(t, vererr)

	priv2, pub2, err := crypto.GenerateKeyPair()

	assert.NoError(t, err, err)
	assert.NotNil(t, priv2)
	assert.NotNil(t, pub2)

	s, err = priv2.Sign(ppm)
	assert.NoError(t, err, "cant sign ", err)
	ssign = hex.EncodeToString(s)

	pm.Metadata.AuthorSign = ssign

	vererr = AuthAuthor(pm)
	assert.Error(t, vererr)
}

func TestSwarm_SignAuth(t *testing.T) {
	n, _ := node.GenerateTestNode(t)
	pm := &pb.ProtocolMessage{
		Metadata: NewProtocolMessageMetadata(n.PublicKey(), "EX", false),
		Payload:  []byte("EX"),
	}

	err := SignMessage(n.PrivateKey(), pm)
	assert.NoError(t, err)

	err = AuthAuthor(pm)

	assert.NoError(t, err)
}
