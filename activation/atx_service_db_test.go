package activation

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

func newTestDbAtxService(t *testing.T) *dbAtxService {
	return NewDBAtxService(
		statesql.InMemoryTest(t),
		types.RandomATXID(),
		atxsdata.New(),
		NewMocknipostValidator(gomock.NewController(t)),
		zaptest.NewLogger(t),
	)
}

// Test if PositioningAtx disregards ATXs with invalid POST in their chain.
// It should pick an ATX with valid POST even though it's a lower height.
func TestGetPositioningAtxPicksAtxWithValidChain(t *testing.T) {
	atxSvc := newTestDbAtxService(t)

	// Invalid chain with high height
	sigInvalid, err := signing.NewEdSigner()
	require.NoError(t, err)
	invalidAtx := newInitialATXv1(t, atxSvc.golden)
	invalidAtx.Sign(sigInvalid)
	vInvalidAtx := toAtx(t, invalidAtx)
	vInvalidAtx.TickCount = 100
	require.NoError(t, err)
	require.NoError(t, atxs.Add(atxSvc.db, vInvalidAtx, invalidAtx.Blob()))
	atxSvc.atxsdata.AddFromAtx(vInvalidAtx, false)

	// Valid chain with lower height
	sigValid, err := signing.NewEdSigner()
	require.NoError(t, err)
	validAtx := newInitialATXv1(t, atxSvc.golden)
	validAtx.NumUnits += 10
	validAtx.Sign(sigValid)
	vValidAtx := toAtx(t, validAtx)
	require.NoError(t, atxs.Add(atxSvc.db, vValidAtx, validAtx.Blob()))
	atxSvc.atxsdata.AddFromAtx(vValidAtx, false)

	atxSvc.validator.(*MocknipostValidator).EXPECT().
		VerifyChain(gomock.Any(), invalidAtx.ID(), atxSvc.golden, gomock.Any()).
		Return(errors.New("this is invalid"))
	atxSvc.validator.(*MocknipostValidator).EXPECT().
		VerifyChain(gomock.Any(), validAtx.ID(), atxSvc.golden, gomock.Any())

	posAtxID, err := atxSvc.PositioningATX(context.Background(), validAtx.PublishEpoch)
	require.NoError(t, err)
	require.Equal(t, vValidAtx.ID(), posAtxID)

	// look in a later epoch, it should return the same one (there is no newer one).
	atxSvc.validator.(*MocknipostValidator).EXPECT().
		VerifyChain(gomock.Any(), invalidAtx.ID(), atxSvc.golden, gomock.Any()).
		Return(errors.New(""))
	atxSvc.validator.(*MocknipostValidator).EXPECT().
		VerifyChain(gomock.Any(), validAtx.ID(), atxSvc.golden, gomock.Any())

	posAtxID, err = atxSvc.PositioningATX(context.Background(), validAtx.PublishEpoch+1)
	require.NoError(t, err)
	require.Equal(t, vValidAtx.ID(), posAtxID)

	// it returns the golden ATX if couldn't find a better one
	posAtxID, err = atxSvc.PositioningATX(context.Background(), validAtx.PublishEpoch-1)
	require.NoError(t, err)
	require.Equal(t, atxSvc.golden, posAtxID)
}

func TestFindFullyValidHighTickAtx(t *testing.T) {
	t.Parallel()
	golden := types.RandomATXID()

	t.Run("skips malicious ATXs", func(t *testing.T) {
		data := atxsdata.New()
		atxMal := &types.ActivationTx{TickCount: 100, SmesherID: types.RandomNodeID()}
		atxMal.SetID(types.RandomATXID())
		data.AddFromAtx(atxMal, true)

		atxLower := &types.ActivationTx{TickCount: 10, SmesherID: types.RandomNodeID()}
		atxLower.SetID(types.RandomATXID())
		data.AddFromAtx(atxLower, false)

		mValidator := NewMocknipostValidator(gomock.NewController(t))
		mValidator.EXPECT().VerifyChain(gomock.Any(), atxLower.ID(), golden, gomock.Any())

		lg := zaptest.NewLogger(t)
		found, err := findFullyValidHighTickAtx(context.Background(), data, 0, golden, mValidator, lg)
		require.NoError(t, err)
		require.Equal(t, atxLower.ID(), found)
	})
}
