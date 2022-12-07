package blocks

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/blocks/mocks"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/hare"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/sql/proposals"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
)

func testConfig() Config {
	return Config{
		GenBlockInterval: 10 * time.Millisecond,
	}
}

type testGenerator struct {
	*Generator
	mockMesh   *mocks.MockmeshProvider
	mockCState *mocks.MockconservativeState
	mockFetch  *smocks.MockProposalFetcher
	mockCert   *mocks.Mockcertifier
}

func createTestGenerator(t *testing.T) *testGenerator {
	types.SetLayersPerEpoch(3)
	ctrl := gomock.NewController(t)
	tg := &testGenerator{
		mockMesh:   mocks.NewMockmeshProvider(ctrl),
		mockCState: mocks.NewMockconservativeState(ctrl),
		mockFetch:  smocks.NewMockProposalFetcher(ctrl),
		mockCert:   mocks.NewMockcertifier(ctrl),
	}
	lg := logtest.New(t)
	cdb := datastore.NewCachedDB(sql.InMemory(), lg)
	tg.Generator = NewGenerator(cdb, tg.mockCState, tg.mockMesh, tg.mockFetch, tg.mockCert,
		WithGeneratorLogger(lg),
		WithHareOutputChan(make(chan hare.LayerOutput, 100)),
		WithConfig(testConfig()))
	return tg
}

func createProposals(t *testing.T, db sql.Executor, layerID types.LayerID, numProposals int) []*types.Proposal {
	t.Helper()
	// make sure proposals have overlapping transactions
	plist := make([]*types.Proposal, 0, numProposals)
	for i := 0; i < numProposals; i++ {
		p := createProposal(t, layerID)
		plist = append(plist, p)
		require.NoError(t, proposals.Add(db, p))
		require.NoError(t, ballots.Add(db, &p.Ballot))
	}
	return plist
}

func createProposal(t *testing.T, lid types.LayerID) *types.Proposal {
	t.Helper()
	p := &types.Proposal{
		InnerProposal: types.InnerProposal{
			Ballot: types.Ballot{
				InnerBallot: types.InnerBallot{
					AtxID:      types.RandomATXID(),
					LayerIndex: lid,
				},
			},
		},
	}
	signer := signing.NewEdSigner()
	p.Ballot.Signature = signer.Sign(p.Ballot.SignedBytes())
	p.Signature = signer.Sign(p.Bytes())
	require.NoError(t, p.Initialize())
	return p
}

func Test_StartStop(t *testing.T) {
	tg := createTestGenerator(t)
	tg.Start()
	tg.Start() // start for the second time is ok.
	tg.Stop()
}

func Test_SerialProcessing(t *testing.T) {
	tg := createTestGenerator(t)
	tg.Start()
	defer tg.Stop()

	numLayers := 4
	var wg sync.WaitGroup
	wg.Add(numLayers)
	for i := uint32(1); i <= uint32(numLayers); i++ {
		lid := types.NewLayerID(i)
		tg.mockCert.EXPECT().RegisterForCert(gomock.Any(), lid, types.EmptyBlockID).Return(nil)
		tg.mockCert.EXPECT().CertifyIfEligible(gomock.Any(), gomock.Any(), lid, types.EmptyBlockID).Return(nil)
		tg.mockMesh.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), lid, types.EmptyBlockID, false).Do(
			func(_ context.Context, gotL types.LayerID, gotB types.BlockID, _ bool) error {
				require.NoError(t, layers.SetApplied(tg.cdb, gotL, gotB))
				wg.Done()
				return nil
			})
	}

	tg.hareCh <- hare.LayerOutput{
		Ctx:   context.TODO(),
		Layer: types.NewLayerID(3),
	}
	tg.hareCh <- hare.LayerOutput{
		Ctx:   context.TODO(),
		Layer: types.NewLayerID(4),
	}
	tg.hareCh <- hare.LayerOutput{
		Ctx:   context.TODO(),
		Layer: types.NewLayerID(2),
	}
	tg.hareCh <- hare.LayerOutput{
		Ctx:   context.TODO(),
		Layer: types.NewLayerID(1),
	}

	wg.Wait()
}

func Test_processHareOutput(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	numProposals := 10
	plist := createProposals(t, tg.cdb, layerID, numProposals)
	pids := types.ToProposalIDs(plist)
	block := types.NewExistingBlock(types.BlockID{1, 2, 3}, types.InnerBlock{LayerIndex: layerID})
	tg.mockFetch.EXPECT().GetProposals(gomock.Any(), pids).Return(nil)
	tg.mockCState.EXPECT().GenerateBlock(gomock.Any(), layerID, plist).Return(block, false, nil)
	tg.mockMesh.EXPECT().AddBlockWithTXs(gomock.Any(), block).Return(nil)
	tg.mockCert.EXPECT().RegisterForCert(gomock.Any(), layerID, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ types.LayerID, got types.BlockID) error {
			require.Equal(t, block.ID(), got)
			return nil
		})
	tg.mockCert.EXPECT().CertifyIfEligible(gomock.Any(), gomock.Any(), layerID, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ log.Log, _ types.LayerID, got types.BlockID) error {
			require.Equal(t, block.ID(), got)
			return nil
		})
	tg.mockMesh.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), layerID, gomock.Any(), false).DoAndReturn(
		func(_ context.Context, _ types.LayerID, got types.BlockID, _ bool) error {
			require.Equal(t, block.ID(), got)
			return nil
		})
	require.NoError(t, tg.processHareOutput(hare.LayerOutput{Ctx: context.TODO(), Layer: layerID, Proposals: pids}))
}

func Test_processHareOutput_EmptyOutput(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	tg.mockCert.EXPECT().RegisterForCert(gomock.Any(), layerID, types.EmptyBlockID).Return(nil)
	tg.mockCert.EXPECT().CertifyIfEligible(gomock.Any(), gomock.Any(), layerID, types.EmptyBlockID).Return(nil)
	tg.mockMesh.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), layerID, types.EmptyBlockID, false).Return(nil)
	require.NoError(t, tg.processHareOutput(hare.LayerOutput{Ctx: context.TODO(), Layer: layerID}))
}

func Test_processHareOutput_ProcessFailed(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	numProposals := 10
	plist := createProposals(t, tg.cdb, layerID, numProposals)
	pids := types.ToProposalIDs(plist)
	block := types.NewExistingBlock(types.BlockID{1, 2, 3}, types.InnerBlock{LayerIndex: layerID})
	tg.mockFetch.EXPECT().GetProposals(gomock.Any(), pids).Return(nil)
	tg.mockCState.EXPECT().GenerateBlock(gomock.Any(), layerID, plist).Return(block, false, nil)
	tg.mockMesh.EXPECT().AddBlockWithTXs(gomock.Any(), block).Return(nil)
	tg.mockCert.EXPECT().RegisterForCert(gomock.Any(), layerID, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ types.LayerID, got types.BlockID) error {
			require.Equal(t, block.ID(), got)
			return nil
		})
	tg.mockCert.EXPECT().CertifyIfEligible(gomock.Any(), gomock.Any(), layerID, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ log.Log, _ types.LayerID, got types.BlockID) error {
			require.Equal(t, block.ID(), got)
			return nil
		})
	errUnknown := errors.New("unknown")
	tg.mockMesh.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), layerID, gomock.Any(), false).Return(errUnknown)
	require.ErrorIs(t, tg.processHareOutput(hare.LayerOutput{Ctx: context.TODO(), Layer: layerID, Proposals: pids}), errUnknown)
}

func Test_processHareOutput_AddBlockFailed(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	numProposals := 10
	plist := createProposals(t, tg.cdb, layerID, numProposals)
	pids := types.ToProposalIDs(plist)
	block := types.NewExistingBlock(types.BlockID{1, 2, 3}, types.InnerBlock{LayerIndex: layerID})
	tg.mockFetch.EXPECT().GetProposals(gomock.Any(), pids).Return(nil)
	tg.mockCState.EXPECT().GenerateBlock(gomock.Any(), layerID, plist).Return(block, false, nil)
	errUnknown := errors.New("unknown")
	tg.mockMesh.EXPECT().AddBlockWithTXs(gomock.Any(), block).Return(errUnknown)
	require.ErrorIs(t, tg.processHareOutput(hare.LayerOutput{Ctx: context.TODO(), Layer: layerID, Proposals: pids}), errUnknown)
}

func Test_processHareOutput_TxPoolFailed(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	numProposals := 10
	plist := createProposals(t, tg.cdb, layerID, numProposals)
	pids := types.ToProposalIDs(plist)
	tg.mockFetch.EXPECT().GetProposals(gomock.Any(), pids).Return(nil)
	errUnknown := errors.New("unknown")
	tg.mockCState.EXPECT().GenerateBlock(gomock.Any(), layerID, plist).Return(nil, false, errUnknown)
	require.ErrorIs(t, tg.processHareOutput(hare.LayerOutput{Ctx: context.TODO(), Layer: layerID, Proposals: pids}), errUnknown)
}

func Test_processHareOutput_FetchFailed(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	numProposals := 10
	plist := createProposals(t, tg.cdb, layerID, numProposals)
	pids := types.ToProposalIDs(plist)
	errUnknown := errors.New("unknown")
	tg.mockFetch.EXPECT().GetProposals(gomock.Any(), pids).Return(errUnknown)
	require.ErrorIs(t, tg.processHareOutput(hare.LayerOutput{Ctx: context.TODO(), Layer: layerID, Proposals: pids}), errUnknown)
}
