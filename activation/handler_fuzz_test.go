package activation

import (
	"context"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p"
	pubsubmocks "github.com/spacemeshos/go-spacemesh/p2p/pubsub/mocks"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
)

type testAtxHandler struct {
	*Handler
	cdb          *datastore.CachedDB
	verifier     *signing.EdVerifier
	testTickSize uint64
	goldenATXID  types.ATXID
	poetConfig   PoetConfig

	mclock     *MocklayerClock
	mpub       *pubsubmocks.MockPublisher
	mfetcher   *smocks.MockFetcher
	mvalidator *MocknipostValidator
	mreceiver  *MockAtxReceiver
}

func newTestAtxHandler(t testing.TB) *testAtxHandler {
	layers := types.GetLayersPerEpoch()
	types.SetLayersPerEpoch(layersPerEpochBig)
	t.Cleanup(func() { types.SetLayersPerEpoch(layers) })

	lg := logtest.New(t)
	cdb := datastore.NewCachedDB(sql.InMemory(), lg)

	verifier, err := signing.NewEdVerifier()
	require.NoError(t, err)

	ctrl := gomock.NewController(t)
	mvalidator := NewMocknipostValidator(ctrl)
	mreceiver := NewMockAtxReceiver(ctrl)
	mclock := NewMocklayerClock(ctrl)
	mpub := pubsubmocks.NewMockPublisher(ctrl)
	mfetcher := smocks.NewMockFetcher(ctrl)

	testTickSize := uint64(1)
	goldenATXID := types.ATXID{2, 3, 4}
	poetCfg := PoetConfig{}

	return &testAtxHandler{
		Handler:      NewHandler(cdb, verifier, mclock, mpub, mfetcher, layersPerEpochBig, testTickSize, goldenATXID, mvalidator, []AtxReceiver{mreceiver}, lg, poetCfg),
		cdb:          cdb,
		verifier:     verifier,
		testTickSize: testTickSize,
		goldenATXID:  goldenATXID,
		poetConfig:   poetCfg,

		mclock:     mclock,
		mpub:       mpub,
		mfetcher:   mfetcher,
		mvalidator: mvalidator,
		mreceiver:  mreceiver,
	}
}

func Fuzz_Handler(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		h := newTestAtxHandler(t)
		h.mclock.EXPECT().LayerToTime(gomock.Any()).Return(time.Now()).AnyTimes()

		h.handleAtxData(context.Background(), p2p.NoPeer, data)
	})
}
