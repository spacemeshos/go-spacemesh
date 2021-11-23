package blocks

import (
	"context"
	"fmt"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/system/mocks"
)

func init() {
	types.SetLayersPerEpoch(3)
}

var initialPost = &types.Post{
	Nonce:   0,
	Indices: []byte(nil),
}

var goldenATXID = types.ATXID(types.HexToHash32("77777"))

func newActivationTx(nodeID types.NodeID, sequence uint64, prevATX types.ATXID, pubLayerID types.LayerID,
	startTick uint64, positioningATX types.ATXID, coinbase types.Address, nipost *types.NIPost) *types.ActivationTx {
	nipostChallenge := types.NIPostChallenge{
		NodeID:         nodeID,
		Sequence:       sequence,
		PrevATXID:      prevATX,
		PubLayerID:     pubLayerID,
		StartTick:      startTick,
		PositioningATX: positioningATX,
	}
	return types.NewActivationTx(nipostChallenge, coinbase, nipost, 1024, nil)
}

func atx(pubkey string) *types.ActivationTx {
	coinbase := types.HexToAddress("aaaa")
	chlng := types.HexToHash32("0x3333")
	poetRef := []byte{0xde, 0xad}
	npst := activation.NewNIPostWithChallenge(&chlng, poetRef)

	atx := newActivationTx(types.NodeID{Key: pubkey, VRFPublicKey: []byte(rand.String(8))}, 0, *types.EmptyATXID, types.NewLayerID(5), 1, goldenATXID, coinbase, npst)
	atx.InitialPost = initialPost
	atx.InitialPostIndices = initialPost.Indices
	atx.CalcAndSetID()
	return atx
}

func genByte32() [32]byte {
	var x [32]byte
	rand.Read(x[:])
	return x
}

var (
	txid1 = types.TransactionID(genByte32())
	txid2 = types.TransactionID(genByte32())
	txid3 = types.TransactionID(genByte32())
)

var (
	one   = types.CalcHash32([]byte("1"))
	two   = types.CalcHash32([]byte("2"))
	three = types.CalcHash32([]byte("3"))
)

var (
	atx1 = types.ATXID(one)
	atx2 = types.ATXID(two)
	atx3 = types.ATXID(three)
)

type meshMock struct{}

func (m meshMock) ForBlockInView(view map[types.BlockID]struct{}, layer types.LayerID, blockHandler func(block *types.Block) (bool, error)) error {
	panic("implement me")
}

func (m meshMock) GetBlock(types.BlockID) (*types.Block, error) {
	panic("implement me")
}

func (m meshMock) AddBlockWithTxs(context.Context, *types.Block) error {
	panic("implement me")
}

func (m meshMock) ProcessedLayer() types.LayerID {
	panic("implement me")
}

func (m meshMock) HandleLateBlock(context.Context, *types.Block) {
	panic("implement me")
}

type verifierMock struct{}

func (v verifierMock) BlockSignedAndEligible(*types.Block) (bool, error) {
	return true, nil
}

func Test_validateUniqueTxAtx(t *testing.T) {
	r := require.New(t)
	b := &types.Block{}

	// unique
	b.TxIDs = []types.TransactionID{txid1, txid2, txid3}
	b.ActiveSet = &[]types.ATXID{atx1, atx2, atx3}
	r.Nil(validateUniqueTxAtx(b))

	// dup txs
	b.TxIDs = []types.TransactionID{txid1, txid2, txid1}
	b.ActiveSet = &[]types.ATXID{atx1, atx2, atx3}
	r.EqualError(validateUniqueTxAtx(b), errDupTx.Error())

	// dup atxs
	b.TxIDs = []types.TransactionID{txid1, txid2, txid3}
	b.ActiveSet = &[]types.ATXID{atx1, atx2, atx1}
	r.EqualError(validateUniqueTxAtx(b), errDupAtx.Error())
}

func TestBlockHandler_BlockSyntacticValidation(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockFetch := mocks.NewMockFetcher(ctrl)

	r := require.New(t)

	s := NewBlockHandler(mockFetch, &meshMock{}, &verifierMock{})
	b := &types.Block{}

	err := s.blockSyntacticValidation(context.TODO(), b)
	r.ErrorIs(err, errNoActiveSet)

	b.ActiveSet = &[]types.ATXID{}
	err = s.blockSyntacticValidation(context.TODO(), b)
	r.ErrorIs(err, errZeroActiveSet)

	b.ActiveSet = &[]types.ATXID{atx1, atx2, atx3}
	b.TxIDs = []types.TransactionID{txid1, txid2, txid1}
	mockFetch.EXPECT().GetAtxs(gomock.Any(), gomock.Any()).Return(nil).Times(1)
	err = s.blockSyntacticValidation(context.TODO(), b)
	r.ErrorIs(err, errDupTx)
}

func TestBlockHandler_BlockSyntacticValidation_InvalidExceptions(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	var (
		ctx   = context.TODO()
		fetch = mocks.NewMockFetcher(ctrl)
		max   = 4
		b     = NewBlockHandler(fetch, &meshMock{}, &verifierMock{}, WithMaxExceptions(max))
	)
	for _, tc := range []struct {
		desc                      string
		support, against, neutral []types.BlockID
		err                       error
	}{
		{
			desc:    "Overflow",
			err:     errExceptionsOverlow,
			support: []types.BlockID{{1}, {2}, {3}, {4}, {5}},
		},
		{
			desc:    "ConflictSupportAgainst",
			err:     errConflictingExceptions,
			support: []types.BlockID{{1}},
			against: []types.BlockID{{1}},
		},
		{
			desc:    "ConflictAgainstAbstain",
			err:     errConflictingExceptions,
			neutral: []types.BlockID{{1}},
			against: []types.BlockID{{1}},
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			block := &types.Block{
				MiniBlock: types.MiniBlock{
					BlockHeader: types.BlockHeader{
						ForDiff:     tc.support,
						AgainstDiff: tc.against,
						NeutralDiff: tc.neutral,
					},
				},
			}
			require.ErrorIs(t, b.blockSyntacticValidation(ctx, block), tc.err)
		})
	}
}

func TestBlockHandler_BlockSyntacticValidation_syncRefBlock(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockFetch := mocks.NewMockFetcher(ctrl)

	r := require.New(t)
	atxpool := activation.NewAtxMemPool()
	s := NewBlockHandler(mockFetch, &meshMock{}, &verifierMock{})
	a := atx("")
	atxpool.Put(a)
	b := &types.Block{}
	b.TxIDs = []types.TransactionID{}
	block1 := types.NewExistingBlock(types.NewLayerID(1), []byte(rand.String(8)), nil)
	block1.ActiveSet = &[]types.ATXID{a.ID()}
	block1.ATXID = a.ID()
	block1.Initialize()
	block1ID := block1.ID()
	b.RefBlock = &block1ID
	b.ATXID = a.ID()
	mockFetch.EXPECT().FetchBlock(gomock.Any(), block1ID).Return(fmt.Errorf("error")).Times(1)
	err := s.blockSyntacticValidation(context.TODO(), b)
	r.Equal(err, fmt.Errorf("failed to fetch ref block %v e: error", *b.RefBlock))

	mockFetch.EXPECT().FetchBlock(gomock.Any(), block1ID).Return(nil).Times(1)
	mockFetch.EXPECT().GetAtxs(gomock.Any(), gomock.Any()).Return(nil).Times(1)
	mockFetch.EXPECT().GetBlocks(gomock.Any(), gomock.Any()).Return(nil).Times(1)
	err = s.blockSyntacticValidation(context.TODO(), b)
	r.NoError(err)
}

func TestBlockHandler_AtxSetID(t *testing.T) {
	a := atx("")
	bbytes, err := types.InterfaceToBytes(*a)
	require.NoError(t, err)
	var b types.ActivationTx
	types.BytesToInterface(bbytes, &b)

	assert.Equal(t, b.NIPost, a.NIPost)
	assert.Equal(t, b.InitialPost, a.InitialPost)

	assert.Equal(t, b.ActivationTxHeader.NodeID, a.ActivationTxHeader.NodeID)
	assert.Equal(t, b.ActivationTxHeader.PrevATXID, a.ActivationTxHeader.PrevATXID)
	assert.Equal(t, b.ActivationTxHeader.Coinbase, a.ActivationTxHeader.Coinbase)
	assert.Equal(t, b.ActivationTxHeader.InitialPostIndices, a.ActivationTxHeader.InitialPostIndices)
	assert.Equal(t, b.ActivationTxHeader.NIPostChallenge, a.ActivationTxHeader.NIPostChallenge)
	b.CalcAndSetID()
	assert.Equal(t, a.ShortString(), b.ShortString())
}
