package activation

import (
	"context"
	"testing"

	"github.com/spacemeshos/post/shared"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"golang.org/x/exp/maps"

	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/nipost"
)

func TestBuilder_BuildsInitialAtxV2(t *testing.T) {
	tab := newTestBuilder(t, 1,
		WithPoetConfig(PoetConfig{RegistrationConfig: RegistrationConfig{PhaseShift: layerDuration}}),
		BuilderAtxVersions(AtxVersions{1: types.AtxV2}))

	tab.SetCoinbase(types.Address{1, 2, 3, 4})
	sig := maps.Values(tab.signers)[0]

	initialPost := types.Post{
		Nonce:   7,
		Indices: []byte{1, 2, 3, 4},
		Pow:     12,
	}
	commitment := tab.goldenATXID
	post := nipost.Post{
		Nonce:         initialPost.Nonce,
		Indices:       initialPost.Indices,
		Pow:           initialPost.Pow,
		Challenge:     shared.ZeroChallenge,
		NumUnits:      100,
		CommitmentATX: commitment,
		VRFNonce:      900,
	}
	err := nipost.AddPost(tab.localDB, sig.NodeID(), post)
	require.NoError(t, err)
	posEpoch := types.EpochID(0)
	layer := posEpoch.FirstLayer()

	tab.mclock.EXPECT().CurrentLayer().Return(layer).Times(4)
	tab.mValidator.EXPECT().PostV2(gomock.Any(), sig.NodeID(), commitment, &initialPost, post.Challenge, post.NumUnits)

	var atx wire.ActivationTxV2
	publishAtx(t, tab, sig.NodeID(), posEpoch, &layer, layersPerEpoch,
		func(_ context.Context, _ string, got []byte) error {
			require.NoError(t, codec.Decode(got, &atx))

			atxHandler := newTestHandler(t, tab.goldenATXID, WithAtxVersions(AtxVersions{1: types.AtxV2}))
			atxHandler.expectInitialAtxV2(&atx)
			require.NoError(t, atxHandler.HandleGossipAtx(context.Background(), "", got))
			return nil
		})
	require.Empty(t, atx.PreviousATXs)
	require.Equal(t, tab.goldenATXID, atx.PositioningATX)
	require.NotNil(t, atx.Initial)
	require.Equal(t, tab.Coinbase(), atx.Coinbase)
	require.Equal(t, initialPost, *wire.PostFromWireV1(&atx.Initial.Post))
	require.Equal(t, commitment, atx.Initial.CommitmentATX)
	require.Nil(t, atx.MarriageATX)
	require.Empty(t, atx.Marriages)
	require.Equal(t, posEpoch+1, atx.PublishEpoch)
	require.Equal(t, sig.NodeID(), atx.SmesherID)
	require.True(t, signing.NewEdVerifier().Verify(signing.ATX, atx.SmesherID, atx.SignedBytes(), atx.Signature))
}

func TestBuilder_SwitchesToBuildV2(t *testing.T) {
	tab := newTestBuilder(t, 1,
		WithPoetConfig(PoetConfig{RegistrationConfig: RegistrationConfig{PhaseShift: layerDuration}}),
		BuilderAtxVersions(AtxVersions{4: types.AtxV2}))
	sig := maps.Values(tab.signers)[0]

	prevAtx := newInitialATXv1(t, tab.goldenATXID)
	prevAtx.Sign(sig)
	require.NoError(t, atxs.Add(tab.db, toAtx(t, prevAtx), prevAtx.Blob()))

	posEpoch := prevAtx.PublishEpoch
	layer := posEpoch.FirstLayer()

	// create and publish ATX V1
	tab.mclock.EXPECT().CurrentLayer().Return(layer).Times(4)
	atx1 := publishAtxV1(t, tab, sig.NodeID(), posEpoch, &layer, layersPerEpoch)
	require.NotNil(t, atx1)

	// create and publish ATX V2
	posEpoch += 1
	layer = posEpoch.FirstLayer()
	tab.mclock.EXPECT().CurrentLayer().Return(layer).Times(4)
	tab.mValidator.EXPECT().VerifyChain(gomock.Any(), atx1.ID(), tab.goldenATXID, gomock.Any())
	var atx2 wire.ActivationTxV2
	publishAtx(t, tab, sig.NodeID(), posEpoch, &layer, layersPerEpoch,
		func(_ context.Context, _ string, got []byte) error {
			return codec.Decode(got, &atx2)
		})
	require.Equal(t, atx1.ID(), atx2.PreviousATXs[0])
	require.Equal(t, atx1.ID(), atx2.PositioningATX)
	require.Nil(t, atx2.Initial)
	require.Nil(t, atx2.MarriageATX)
	require.Empty(t, atx2.Marriages)
	require.Equal(t, atx1.PublishEpoch+1, atx2.PublishEpoch)
	require.Equal(t, sig.NodeID(), atx2.SmesherID)
	require.True(t, signing.NewEdVerifier().Verify(signing.ATX, atx2.SmesherID, atx2.SignedBytes(), atx2.Signature))
}
