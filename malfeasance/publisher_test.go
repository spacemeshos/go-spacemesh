package malfeasance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/malfeasance/wire"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub/mocks"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

type testMalPublisher struct {
	*Publisher

	mSyncer    *Mocksyncer
	mTortoise  *Mocktortoise
	mPublisher *mocks.MockPublisher
}

func newTestPublisher(tb testing.TB) *testMalPublisher {
	logger := zaptest.NewLogger(tb)

	db := statesql.InMemoryTest(tb)
	cdb := datastore.NewCachedDB(db, logger)

	ctrl := gomock.NewController(tb)
	mSyncer := NewMocksyncer(ctrl)
	mTortoise := NewMocktortoise(ctrl)
	mPublisher := mocks.NewMockPublisher(ctrl)

	publisher := NewPublisher(
		logger,
		cdb,
		mSyncer,
		mTortoise,
		mPublisher,
	)

	return &testMalPublisher{
		Publisher: publisher,

		mSyncer:    mSyncer,
		mTortoise:  mTortoise,
		mPublisher: mPublisher,
	}
}

func TestMalfeasancePublisher(t *testing.T) {
	t.Run("PublishProof when in sync", func(t *testing.T) {
		malPublisher := newTestPublisher(t)

		nodeID := types.RandomNodeID()
		proof := &wire.MalfeasanceProof{
			Layer: 1,
			Proof: wire.Proof{
				Type: wire.MultipleATXs,
				Data: &wire.AtxProof{},
			},
		}

		malPublisher.mTortoise.EXPECT().OnMalfeasance(nodeID)
		malPublisher.mSyncer.EXPECT().ListenToATXGossip().Return(true)
		malPublisher.mPublisher.EXPECT().
			Publish(context.Background(), pubsub.MalfeasanceProof, gomock.Any()).
			DoAndReturn(func(ctx context.Context, s string, b []byte) error {
				var gossip wire.MalfeasanceGossip
				codec.MustDecode(b, &gossip)
				require.Equal(t, *proof, gossip.MalfeasanceProof)
				return nil
			})

		err := malPublisher.PublishProof(context.Background(), nodeID, proof)
		require.NoError(t, err)

		malicious, err := identities.IsMalicious(malPublisher.cdb, nodeID)
		require.NoError(t, err)
		require.True(t, malicious)
	})

	t.Run("PublishProof when not in sync", func(t *testing.T) {
		malPublisher := newTestPublisher(t)

		nodeID := types.RandomNodeID()
		proof := &wire.MalfeasanceProof{
			Layer: 1,
			Proof: wire.Proof{
				Type: wire.MultipleATXs,
				Data: &wire.AtxProof{},
			},
		}

		malPublisher.mTortoise.EXPECT().OnMalfeasance(nodeID)
		malPublisher.mSyncer.EXPECT().ListenToATXGossip().Return(false)

		err := malPublisher.PublishProof(context.Background(), nodeID, proof)
		require.NoError(t, err)

		// proof is only persisted but not published
		malicious, err := identities.IsMalicious(malPublisher.cdb, nodeID)
		require.NoError(t, err)
		require.True(t, malicious)
	})

	t.Run("PublishProof when already marked as malicious", func(t *testing.T) {
		malPublisher := newTestPublisher(t)

		nodeID := types.RandomNodeID()
		existingProof := &wire.MalfeasanceProof{
			Layer: 1,
			Proof: wire.Proof{
				Type: wire.MultipleATXs,
				Data: &wire.AtxProof{},
			},
		}

		err := identities.SetMalicious(malPublisher.cdb, nodeID, codec.MustEncode(existingProof), time.Now())
		require.NoError(t, err)

		proof := &wire.MalfeasanceProof{
			Layer: 11,
			Proof: wire.Proof{
				Type: wire.MultipleBallots,
				Data: &wire.BallotProof{},
			},
		}
		err = malPublisher.PublishProof(context.Background(), nodeID, proof)
		require.NoError(t, err)

		// no new malfeasance proof is added
		var blob sql.Blob
		err = identities.LoadMalfeasanceBlob(context.Background(), malPublisher.cdb, nodeID.Bytes(), &blob)
		require.NoError(t, err)

		dbProof := &wire.MalfeasanceProof{}
		codec.MustDecode(blob.Bytes, dbProof)

		require.Equal(t, existingProof, dbProof)
	})

	t.Run("PublishProof when error occurs", func(t *testing.T) {
		malPublisher := newTestPublisher(t)

		nodeID := types.RandomNodeID()
		proof := &wire.MalfeasanceProof{
			Layer: 1,
			Proof: wire.Proof{
				Type: wire.MultipleATXs,
				Data: &wire.AtxProof{},
			},
		}

		malPublisher.mTortoise.EXPECT().OnMalfeasance(nodeID)
		malPublisher.mSyncer.EXPECT().ListenToATXGossip().Return(true)
		errPublish := errors.New("publish failed")
		malPublisher.mPublisher.EXPECT().
			Publish(context.Background(), pubsub.MalfeasanceProof, gomock.Any()).
			Return(errPublish)

		err := malPublisher.PublishProof(context.Background(), nodeID, proof)
		require.ErrorIs(t, err, errPublish)

		// malfeasance proof is still added to db
		malicious, err := identities.IsMalicious(malPublisher.cdb, nodeID)
		require.NoError(t, err)
		require.True(t, malicious)
	})
}
