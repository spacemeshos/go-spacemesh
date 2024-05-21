package activation

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"golang.org/x/exp/maps"

	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
)

func TestBuilder_SwitchesToBuildV2(t *testing.T) {
	tab := newTestBuilder(t, 1,
		WithPoetConfig(PoetConfig{PhaseShift: layerDuration}),
		BuilderAtxVersions(AtxVersions{postGenesisEpoch + 2: types.AtxV2}))
	sig := maps.Values(tab.signers)[0]

	prevAtx := newInitialATXv1(t, tab.goldenATXID)
	prevAtx.Sign(sig)
	require.NoError(t, atxs.Add(tab.db, toAtx(t, prevAtx)))

	posEpoch := prevAtx.PublishEpoch
	layer := posEpoch.FirstLayer()

	// create and publish ATX V1
	tab.mclock.EXPECT().CurrentLayer().Return(layer).Times(4)
	atx1 := publishAtxV1(t, tab, sig.NodeID(), posEpoch, &layer, layersPerEpoch)
	require.NotNil(t, atx1)

	// create and publish ATX V2
	posEpoch = postGenesisEpoch + 1
	layer = posEpoch.FirstLayer()
	tab.mclock.EXPECT().CurrentLayer().Return(layer).Times(4)
	tab.mValidator.EXPECT().VerifyChain(gomock.Any(), atx1.ID(), tab.goldenATXID, gomock.Any())
	var atx2 wire.ActivationTxV2
	publishAtx(t, tab, sig.NodeID(), posEpoch, &layer, layersPerEpoch,
		func(_ context.Context, _ string, got []byte) error {
			return codec.Decode(got, &atx2)
		})
	require.NotNil(t, atx2)
	require.Equal(t, atx1.ID(), atx2.PreviousATXs[0])
	require.Equal(t, atx1.ID(), atx2.PositioningATX)
	require.Nil(t, atx2.Initial)
	require.Nil(t, atx2.MarriageATX)
	require.Empty(t, atx2.Marriages)
	require.Equal(t, atx1.PublishEpoch+1, atx2.PublishEpoch)
	require.Equal(t, sig.NodeID(), atx2.SmesherID)
	require.True(t, signing.NewEdVerifier().Verify(signing.ATX, atx2.SmesherID, atx2.SignedBytes(), atx2.Signature))
}
