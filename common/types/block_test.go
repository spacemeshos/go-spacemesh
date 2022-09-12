package types

import (
	"testing"

	"github.com/spacemeshos/ed25519"
	"github.com/spacemeshos/go-scale/tester"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/signing"
)

func TestBlock_IDSize(t *testing.T) {
	var id BlockID
	assert.Len(t, id.Bytes(), BlockIDSize)
}

func Test_CertifyMessage(t *testing.T) {
	msg := CertifyMessage{
		CertifyContent: CertifyContent{
			LayerID:        NewLayerID(11),
			BlockID:        RandomBlockID(),
			EligibilityCnt: 2,
			Proof:          []byte("not a fraud"),
		},
	}
	signer := signing.NewEdSigner()
	msg.Signature = signer.Sign(msg.Bytes())
	data, err := codec.Encode(&msg)
	require.NoError(t, err)

	var decoded CertifyMessage
	require.NoError(t, codec.Decode(data, &decoded))
	require.Equal(t, msg, decoded)
	pubKey, err := ed25519.ExtractPublicKey(decoded.Bytes(), decoded.Signature)
	require.NoError(t, err)
	require.Equal(t, signer.PublicKey().Bytes(), []byte(pubKey))
}

func TestRewardCodec(t *testing.T) {
	weight := util.WeightFromUint64(1234).Div(util.WeightFromUint64(7))
	r := &AnyReward{
		Coinbase: GenerateAddress(RandomBytes(AddressLength)),
		Weight:   RatNum{Num: weight.Num().Uint64(), Denom: weight.Denom().Uint64()},
	}

	data, err := codec.Encode(r)
	require.NoError(t, err)

	var got AnyReward
	require.NoError(t, codec.Decode(data, &got))
	require.Equal(t, r, &got)
}

func FuzzAnyRewardConsistency(f *testing.F) {
	tester.FuzzConsistency[AnyReward](f)
}

func FuzzAnyRewardSafety(f *testing.F) {
	tester.FuzzSafety[AnyReward](f)
}

func FuzzBlockIDConsistency(f *testing.F) {
	tester.FuzzConsistency[BlockID](f)
}

func FuzzBlockIDSafety(f *testing.F) {
	tester.FuzzSafety[BlockID](f)
}

func FuzzRatNumConsistency(f *testing.F) {
	tester.FuzzConsistency[RatNum](f)
}

func FuzzRatNumSafety(f *testing.F) {
	tester.FuzzSafety[RatNum](f)
}

func FuzzBlockConsistency(f *testing.F) {
	tester.FuzzConsistency[Block](f)
}

func FuzzBlockSafety(f *testing.F) {
	tester.FuzzSafety[Block](f)
}

func FuzzBlockContextualValidityConsistency(f *testing.F) {
	tester.FuzzConsistency[BlockContextualValidity](f)
}

func FuzzBlockContextualValiditySafety(f *testing.F) {
	tester.FuzzSafety[BlockContextualValidity](f)
}

func FuzzInnerBlockConsistency(f *testing.F) {
	tester.FuzzConsistency[InnerBlock](f)
}

func FuzzInnerBlockSafety(f *testing.F) {
	tester.FuzzSafety[InnerBlock](f)
}
