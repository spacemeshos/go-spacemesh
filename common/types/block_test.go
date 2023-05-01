package types_test

import (
	"math/big"
	"testing"

	"github.com/spacemeshos/go-scale/tester"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
)

func TestBlock_IDSize(t *testing.T) {
	var id types.BlockID
	assert.Len(t, id.Bytes(), types.BlockIDSize)
}

func Test_CertifyMessage(t *testing.T) {
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)

	msg := types.CertifyMessage{
		CertifyContent: types.CertifyContent{
			LayerID:        types.LayerID(11),
			BlockID:        types.RandomBlockID(),
			EligibilityCnt: 2,
			Proof:          types.RandomVrfSignature(),
		},
		SmesherID: signer.NodeID(),
	}

	msg.Signature = signer.Sign(signing.HARE, msg.Bytes())
	data, err := codec.Encode(&msg)
	require.NoError(t, err)

	var decoded types.CertifyMessage
	require.NoError(t, codec.Decode(data, &decoded))
	require.Equal(t, msg, decoded)

	pke, err := signing.NewEdVerifier()
	require.NoError(t, err)
	ok := pke.Verify(signing.HARE, decoded.SmesherID, msg.Bytes(), decoded.Signature)
	require.True(t, ok)
}

func Test_BlockIDsToHashes(t *testing.T) {
	blockIDs := []types.BlockID{types.RandomBlockID(), types.RandomBlockID()}
	expectedHashes := make([]types.Hash32, 0, len(blockIDs))

	for _, id := range blockIDs {
		expectedHashes = append(expectedHashes, id.AsHash32())
	}

	actualHashes := types.BlockIDsToHashes(blockIDs)
	require.Equal(t, expectedHashes, actualHashes)
}

func Test_NewExistingBlock(t *testing.T) {
	expectedNewExistingBlock := types.NewExistingBlock(types.BlockID{1, 1}, types.InnerBlock{LayerIndex: types.LayerID(1)})

	actualNewExistingBlock := types.NewExistingBlock(
		types.BlockID{1, 1},
		types.InnerBlock{LayerIndex: types.LayerID(1)},
	)

	require.Equal(t, expectedNewExistingBlock, actualNewExistingBlock)
}

func Test_BlockInitialize(t *testing.T) {
	testBlock := types.NewExistingBlock(types.BlockID{1, 1}, types.InnerBlock{LayerIndex: types.LayerID(1)})

	expectedBlockID := types.BlockID(types.CalcHash32(testBlock.Bytes()).ToHash20())
	// Initialize the block for compute actual Block ID
	testBlock.Initialize()
	actualBlockID := testBlock.ID()

	require.Equal(t, expectedBlockID, actualBlockID)
	require.Equal(t, expectedBlockID, testBlock.ID())
}

func Test_BlockBytes(t *testing.T) {
	testBlock := types.NewExistingBlock(types.BlockID{1, 1}, types.InnerBlock{LayerIndex: types.LayerID(1)})

	expectedBytes, err := codec.Encode(&testBlock.InnerBlock)
	require.NoError(t, err)
	actualBytes := testBlock.Bytes()
	require.Equal(t, expectedBytes, actualBytes)

	expectedBytes = testBlock.ID().AsHash32().Bytes()
	actualBytes = testBlock.ID().Bytes()
	require.Equal(t, expectedBytes, actualBytes)
}

func Test_BlockFieldString(t *testing.T) {
	testBlockID := types.BlockID{1, 1}

	expectedField := log.String("block_id", testBlockID.String())
	actualField := testBlockID.Field()
	require.Equal(t, expectedField, actualField)

	expectedIDString := testBlockID.AsHash32().ShortString()
	actualIDString := testBlockID.String()
	require.Equal(t, expectedIDString, actualIDString)
}

func Test_BlockIDCompare(t *testing.T) {
	testBlockID_1 := types.BlockID{1, 1}
	testBlockID_2 := types.BlockID{2, 2}
	testBlockID_3 := types.BlockID{3, 3}

	require.Equal(t, false, testBlockID_2.Compare(testBlockID_2))
	require.Equal(t, false, testBlockID_2.Compare(testBlockID_1))
	require.Equal(t, true, testBlockID_2.Compare(testBlockID_3))
}

func Test_SortBlockIDs(t *testing.T) {
	testBlockIDs := []types.BlockID{{3, 3}, {2, 2}, {1, 1}}
	expectedBlockIDs := []types.BlockID{{1, 1}, {2, 2}, {3, 3}}
	actualBlockIDs := types.SortBlockIDs(testBlockIDs)

	require.Equal(t, expectedBlockIDs, actualBlockIDs)
}

func TestToBlockIDs(t *testing.T) {
	testBlocks := []*types.Block{
		types.NewExistingBlock(types.BlockID{1, 1}, types.InnerBlock{LayerIndex: types.LayerID(1)}),
		types.NewExistingBlock(types.BlockID{2, 2}, types.InnerBlock{LayerIndex: types.LayerID(1)}),
		types.NewExistingBlock(types.BlockID{3, 3}, types.InnerBlock{LayerIndex: types.LayerID(1)}),
	}

	expectedBlockIDs := []types.BlockID{{1, 1}, {2, 2}, {3, 3}}
	actualBlockIDs := types.ToBlockIDs(testBlocks)

	require.Equal(t, expectedBlockIDs, actualBlockIDs)
}

func TestRewardCodec(t *testing.T) {
	weight := big.NewRat(1234, 7)
	r := &types.AnyReward{
		AtxID:  types.RandomATXID(),
		Weight: types.RatNum{Num: weight.Num().Uint64(), Denom: weight.Denom().Uint64()},
	}

	data, err := codec.Encode(r)
	require.NoError(t, err)

	var got types.AnyReward
	require.NoError(t, codec.Decode(data, &got))
	require.Equal(t, r, &got)
}

func FuzzAnyRewardConsistency(f *testing.F) {
	tester.FuzzConsistency[types.AnyReward](f)
}

func FuzzAnyRewardSafety(f *testing.F) {
	tester.FuzzSafety[types.AnyReward](f)
}

func FuzzBlockIDConsistency(f *testing.F) {
	tester.FuzzConsistency[types.BlockID](f)
}

func FuzzBlockIDSafety(f *testing.F) {
	tester.FuzzSafety[types.BlockID](f)
}

func FuzzRatNumConsistency(f *testing.F) {
	tester.FuzzConsistency[types.RatNum](f)
}

func FuzzRatNumSafety(f *testing.F) {
	tester.FuzzSafety[types.RatNum](f)
}

func FuzzBlockConsistency(f *testing.F) {
	tester.FuzzConsistency[types.Block](f)
}

func FuzzBlockSafety(f *testing.F) {
	tester.FuzzSafety[types.Block](f)
}

func FuzzInnerBlockConsistency(f *testing.F) {
	tester.FuzzConsistency[types.InnerBlock](f)
}

func FuzzInnerBlockSafety(f *testing.F) {
	tester.FuzzSafety[types.InnerBlock](f)
}

func TestBlockEncoding(t *testing.T) {
	types.CheckLayerFirstEncoding(t, func(object types.Block) types.LayerID { return object.LayerIndex })
}
