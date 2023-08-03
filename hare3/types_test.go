package hare3

import (
	"testing"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
)

func TestMessageMarshall(t *testing.T) {
	enc := zapcore.NewMapObjectEncoder()
	msg := &Message{Body: Body{Value: Value{Proposals: []types.ProposalID{{}}}}}
	require.NoError(t, msg.MarshalLogObject(enc))
	msg = &Message{Body: Body{Value: Value{Reference: &types.Hash32{}}}}
	require.NoError(t, msg.MarshalLogObject(enc))
}
