package p2p

import (
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/pb"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func Test_NewProtocolMessageMeatadata(t *testing.T) {
	pk := p2pcrypto.NewRandomPubkey()

	assert.NotNil(t, pk)

	meta := NewProtocolMessageMetadata(pk, "EX")

	assert.NotNil(t, meta, "should be a metadata")
	assert.Equal(t, meta.Timestamp, time.Now().Unix())
	assert.Equal(t, meta.ClientVersion, config.ClientVersion)
	assert.Equal(t, meta.AuthPubkey, pk.Bytes())
	assert.Equal(t, meta.NextProtocol, "EX")
}

func TestExtractData(t *testing.T) {
	pk := p2pcrypto.NewRandomPubkey()

	assert.NotNil(t, pk)

	meta := NewProtocolMessageMetadata(pk, "EX")

	msg := &pb.ProtocolMessage{
		Metadata: meta,
		Data:     &pb.ProtocolMessage_Payload{[]byte("test")},
	}
	data1, err := ExtractData(msg)
	require.NoError(t, err)
	_, ok := data1.(*service.DataBytes)
	require.True(t, ok)

	msg2 := &pb.ProtocolMessage{
		Metadata: meta,
		Data:     &pb.ProtocolMessage_Msg{&pb.MessageWrapper{Type: 0, Req: true, ReqID: 0, Payload: []byte("test")}},
	}
	data2, err := ExtractData(msg2)
	require.NoError(t, err)
	_, ok = data2.(*service.DataMsgWrapper)
	require.True(t, ok)

	msg3 := &pb.ProtocolMessage{
		Metadata: meta,
		Data:     nil,
	}

	data3, err := ExtractData(msg3)
	require.Error(t, err)
	require.Nil(t, data3)
}
