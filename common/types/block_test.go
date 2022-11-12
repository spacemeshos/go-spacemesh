package types

import (
	"testing"

	"github.com/spacemeshos/go-scale/tester"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
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
	pubKey, err := signing.ExtractPublicKey(decoded.Bytes(), decoded.Signature)
	require.NoError(t, err)
	require.Equal(t, signer.PublicKey().Bytes(), []byte(pubKey))
}

func Test_BlockIDsToHashes(t *testing.T) {
	blockIDs := []BlockID{RandomBlockID(), RandomBlockID()}
	expectedHashes := make([]Hash32, 0, len(blockIDs))

	for _, id := range blockIDs {
		expectedHashes = append(expectedHashes, id.AsHash32())
	}

	actualHashes := BlockIDsToHashes(blockIDs)
	require.Equal(t, expectedHashes, actualHashes)
}

func Test_NewExistingBlock(t *testing.T) {
	expectedNewExistingBlock := &Block{blockID: BlockID{1, 1}, InnerBlock: InnerBlock{LayerIndex: NewLayerID(1)}}

	actualNewExistingBlock := NewExistingBlock(
		BlockID{1, 1},
		InnerBlock{LayerIndex: NewLayerID(1)},
	)

	require.Equal(t, expectedNewExistingBlock, actualNewExistingBlock)
}

func Test_BlockInitialize(t *testing.T) {
	testBlock := Block{blockID: BlockID{1, 1}, InnerBlock: InnerBlock{LayerIndex: NewLayerID(1)}}

	expectedBlockID := BlockID(CalcHash32(testBlock.Bytes()).ToHash20())
	// Initialize the block for compute actual Block ID
	testBlock.Initialize()
	actualBlockID := testBlock.blockID

	require.Equal(t, expectedBlockID, actualBlockID)
	require.Equal(t, expectedBlockID, testBlock.ID())
}

func Test_BlockBytes(t *testing.T) {
	testBlock := Block{blockID: BlockID{1, 1}, InnerBlock: InnerBlock{LayerIndex: NewLayerID(1)}}

	expectedBytes, err := codec.Encode(&testBlock.InnerBlock)
	require.NoError(t, err)
	actualBytes := testBlock.Bytes()
	require.Equal(t, expectedBytes, actualBytes)

	expectedBytes = testBlock.blockID.AsHash32().Bytes()
	actualBytes = testBlock.blockID.Bytes()
	require.Equal(t, expectedBytes, actualBytes)
}

func Test_BlockFieldString(t *testing.T) {
	testBlockID := BlockID{1, 1}

	expectedField := log.String("block_id", testBlockID.String())
	actualField := testBlockID.Field()
	require.Equal(t, expectedField, actualField)

	expectedIDString := testBlockID.AsHash32().ShortString()
	actualIDString := testBlockID.String()
	require.Equal(t, expectedIDString, actualIDString)
}

func Test_BlockIDCompare(t *testing.T) {
	testBlockID_1 := BlockID{1, 1}
	testBlockID_2 := BlockID{2, 2}
	testBlockID_3 := BlockID{3, 3}

	require.Equal(t, false, testBlockID_2.Compare(testBlockID_2))
	require.Equal(t, false, testBlockID_2.Compare(testBlockID_1))
	require.Equal(t, true, testBlockID_2.Compare(testBlockID_3))
}

func Test_SortBlockIDs(t *testing.T) {
	testBlockIDs := []BlockID{{3, 3}, {2, 2}, {1, 1}}
	expectedBlockIDs := []BlockID{{1, 1}, {2, 2}, {3, 3}}
	actualBlockIDs := SortBlockIDs(testBlockIDs)

	require.Equal(t, expectedBlockIDs, actualBlockIDs)
}

func TestToBlockIDs(t *testing.T) {
	testBlocks := []*Block{
		{blockID: BlockID{1, 1}, InnerBlock: InnerBlock{LayerIndex: NewLayerID(1)}},
		{blockID: BlockID{2, 2}, InnerBlock: InnerBlock{LayerIndex: NewLayerID(1)}},
		{blockID: BlockID{3, 3}, InnerBlock: InnerBlock{LayerIndex: NewLayerID(1)}},
	}

	expectedBlockIDs := []BlockID{{1, 1}, {2, 2}, {3, 3}}
	actualBlockIDs := ToBlockIDs(testBlocks)

	require.Equal(t, expectedBlockIDs, actualBlockIDs)
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
