package hare3

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

func TestAbsoluteMaxValue(t *testing.T) {
	ir := IterRound{Iter: 40, Round: notify}
	require.EqualValues(t, 41*7, ir.Absolute())
}

func TestMessageMarshall(t *testing.T) {
	enc := zapcore.NewMapObjectEncoder()
	msg := &Message{Body: Body{Value: Value{Proposals: []types.ProposalID{{}}}}}
	require.NoError(t, msg.MarshalLogObject(enc))
	msg = &Message{Body: Body{Value: Value{Reference: &types.Hash32{}}}}
	require.NoError(t, msg.MarshalLogObject(enc))
}

func FuzzMessageDecode(f *testing.F) {
	for _, buf := range [][]byte{
		{},
		{0},
		{0, 1, 1},
		{0, 1, 1, 0, 10},
	} {
		f.Add(buf)
	}
	f.Fuzz(func(t *testing.T, buf []byte) {
		var msg Message
		if err := codec.Decode(buf, &msg); err == nil {
			_ = msg.Validate()
		}
	})
}
