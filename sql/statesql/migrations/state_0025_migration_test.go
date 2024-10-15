package migrations

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	gomock "go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
	"go.uber.org/zap/zaptest/observer"

	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	mwire "github.com/spacemeshos/go-spacemesh/malfeasance/wire"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

func Test0025Migration(t *testing.T) {
	setup := func(tb testing.TB) sql.StateDatabase {
		schema, err := statesql.Schema()
		require.NoError(tb, err)
		schema.Migrations = slices.DeleteFunc(schema.Migrations, func(m sql.Migration) bool {
			return m.Order() >= 25
		})
		db := statesql.InMemoryTest(
			tb,
			sql.WithDatabaseSchema(schema),
			sql.WithNoCheckSchemaDrift(),
			sql.WithForceMigrations(true),
		)
		return db
	}

	t.Run("valid proof is noop", func(t *testing.T) {
		db := setup(t)

		// store valid proof
		nodeID := types.RandomNodeID()
		proof := &mwire.MalfeasanceProof{
			Proof: mwire.Proof{
				Type: mwire.MultipleATXs,
				Data: &mwire.AtxProof{},
			},
		}
		require.NoError(t, identities.SetMalicious(db, nodeID, codec.MustEncode(proof), time.Now()))
		ctrl := gomock.NewController(t)
		mHandler := NewMockmalfeasanceValidator(ctrl)
		mHandler.EXPECT().Validate(context.Background(), proof).Return(nodeID, nil)

		m := New0025Migration(mHandler)
		require.Equal(t, 25, m.Order())
		require.NoError(t, m.Apply(db, zaptest.NewLogger(t)))
	})

	t.Run("invalid proof is logged", func(t *testing.T) {
		db := setup(t)

		// store invalid proof
		nodeID := types.RandomNodeID()
		proof := &mwire.MalfeasanceProof{
			Proof: mwire.Proof{
				Type: mwire.MultipleATXs,
				Data: &mwire.AtxProof{},
			},
		}
		require.NoError(t, identities.SetMalicious(db, nodeID, codec.MustEncode(proof), time.Now()))
		ctrl := gomock.NewController(t)
		mHandler := NewMockmalfeasanceValidator(ctrl)
		mHandler.EXPECT().Validate(context.Background(), proof).
			Return(types.EmptyNodeID, errors.New("invalid signature"))

		observer, observedLogs := observer.New(zapcore.WarnLevel)
		logger := zaptest.NewLogger(t, zaptest.WrapOptions(zap.WrapCore(
			func(core zapcore.Core) zapcore.Core {
				return zapcore.NewTee(core, observer)
			},
		)))

		m := New0025Migration(mHandler)
		require.Equal(t, 25, m.Order())
		require.NoError(t, m.Apply(db, logger))

		require.Equal(t, 1, observedLogs.Len(), "expected 1 log message")
		require.Equal(t, zapcore.WarnLevel, observedLogs.All()[0].Level)
		require.Equal(t, "Found invalid proof during migration that cannot be fixed", observedLogs.All()[0].Message)
		require.Equal(t, nodeID.ShortString(), observedLogs.All()[0].ContextMap()["smesherID"])
		require.Equal(t, "invalid signature", observedLogs.All()[0].ContextMap()["error"])
	})

	t.Run("invalid proof is fixed", func(t *testing.T) {
		db := setup(t)

		// store invalid proof
		nonce := uint64(1337)
		nodeID := types.RandomNodeID()
		proof := &mwire.MalfeasanceProof{
			Proof: mwire.Proof{
				Type: mwire.InvalidPrevATX,
				Data: &mwire.InvalidPrevATXProof{
					Atx1: wire.ActivationTxV1{
						InnerActivationTxV1: wire.InnerActivationTxV1{
							VRFNonce: &nonce,
						},
					},
					Atx2: wire.ActivationTxV1{},
				},
			},
		}
		require.NoError(t, identities.SetMalicious(db, nodeID, codec.MustEncode(proof), time.Now()))
		ctrl := gomock.NewController(t)
		mHandler := NewMockmalfeasanceValidator(ctrl)

		// first call to Validate returns error
		mHandler.EXPECT().Validate(context.Background(), proof).
			Return(types.EmptyNodeID, errors.New("invalid signature"))

		// second call to Validate returns valid nodeID
		mHandler.EXPECT().Validate(context.Background(), gomock.Cond(func(x any) bool {
			return x.(*mwire.MalfeasanceProof).Proof.Type == mwire.InvalidPrevATX &&
				x.(*mwire.MalfeasanceProof).Proof.Data.(*mwire.InvalidPrevATXProof).Atx1.VRFNonce == nil
		})).Return(nodeID, nil)

		observer, observedLogs := observer.New(zapcore.InfoLevel)
		logger := zaptest.NewLogger(t, zaptest.WrapOptions(zap.WrapCore(
			func(core zapcore.Core) zapcore.Core {
				return zapcore.NewTee(core, observer)
			},
		)))

		m := New0025Migration(mHandler)
		require.Equal(t, 25, m.Order())
		require.NoError(t, m.Apply(db, logger))

		require.Equal(t, 1, observedLogs.Len(), "expected 1 log message")
		require.Equal(t, zapcore.InfoLevel, observedLogs.All()[0].Level)
		require.Equal(t, "Fixed invalid proof during migration", observedLogs.All()[0].Message)
		require.Equal(t, nodeID.ShortString(), observedLogs.All()[0].ContextMap()["smesherID"])

		// check proof was updated
		blob := &sql.Blob{}
		err := identities.LoadMalfeasanceBlob(context.Background(), db, nodeID.Bytes(), blob)
		require.NoError(t, err)
		updatedProof := &mwire.MalfeasanceProof{}
		codec.MustDecode(blob.Bytes, updatedProof)

		require.NotEqual(t, proof, updatedProof)
		require.Nil(t, updatedProof.Proof.Data.(*mwire.InvalidPrevATXProof).Atx1.VRFNonce)
	})

	t.Run("invalid proof cannot be fixed", func(t *testing.T) {
		db := setup(t)

		// store invalid proof
		nonce := uint64(1337)
		nodeID := types.RandomNodeID()
		proof := &mwire.MalfeasanceProof{
			Proof: mwire.Proof{
				Type: mwire.InvalidPrevATX,
				Data: &mwire.InvalidPrevATXProof{
					Atx1: wire.ActivationTxV1{
						InnerActivationTxV1: wire.InnerActivationTxV1{
							VRFNonce: &nonce,
						},
					},
					Atx2: wire.ActivationTxV1{},
				},
			},
		}
		require.NoError(t, identities.SetMalicious(db, nodeID, codec.MustEncode(proof), time.Now()))
		ctrl := gomock.NewController(t)
		mHandler := NewMockmalfeasanceValidator(ctrl)

		// first call to Validate returns error
		mHandler.EXPECT().Validate(context.Background(), proof).
			Return(types.EmptyNodeID, errors.New("invalid signature"))

		// second call to Validate still returns error
		mHandler.EXPECT().Validate(context.Background(), gomock.Cond(func(x any) bool {
			return x.(*mwire.MalfeasanceProof).Proof.Type == mwire.InvalidPrevATX &&
				x.(*mwire.MalfeasanceProof).Proof.Data.(*mwire.InvalidPrevATXProof).Atx1.VRFNonce == nil
		})).Return(types.EmptyNodeID, errors.New("invalid signature"))

		observer, observedLogs := observer.New(zapcore.ErrorLevel)
		logger := zaptest.NewLogger(t, zaptest.WrapOptions(zap.WrapCore(
			func(core zapcore.Core) zapcore.Core {
				return zapcore.NewTee(core, observer)
			},
		)))

		m := New0025Migration(mHandler)
		require.Equal(t, 25, m.Order())
		require.NoError(t, m.Apply(db, logger))

		require.Equal(t, 1, observedLogs.Len(), "expected 1 log message")
		require.Equal(t, zapcore.ErrorLevel, observedLogs.All()[0].Level)
		require.Equal(t, "Failed to fix invalid malfeasance proof during migration", observedLogs.All()[0].Message)
		require.Equal(t, nodeID.ShortString(), observedLogs.All()[0].ContextMap()["smesherID"])
		require.Equal(t, "invalid signature", observedLogs.All()[0].ContextMap()["error"])

		// check proof not updated
		blob := &sql.Blob{}
		err := identities.LoadMalfeasanceBlob(context.Background(), db, nodeID.Bytes(), blob)
		require.NoError(t, err)
		updatedProof := &mwire.MalfeasanceProof{}
		codec.MustDecode(blob.Bytes, updatedProof)

		require.Equal(t, proof, updatedProof)
	})
}
