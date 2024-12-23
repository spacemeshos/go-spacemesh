package activation

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
	"go.uber.org/zap/zaptest/observer"

	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

type testMalHandler struct {
	*MalfeasanceHandlerV2

	observedLogs *observer.ObservedLogs
	ctrl         *gomock.Controller
	mPublish     *MockmalfeasancePublisher
	mValidator   *MocknipostValidator
}

func newTestMalHandler(tb testing.TB) *testMalHandler {
	edVerifier := signing.NewEdVerifier()

	observer, observedLogs := observer.New(zap.DebugLevel)
	logger := zaptest.NewLogger(tb, zaptest.WrapOptions(zap.WrapCore(
		func(core zapcore.Core) zapcore.Core {
			return zapcore.NewTee(core, observer)
		},
	)))

	ctrl := gomock.NewController(tb)
	mPublish := NewMockmalfeasancePublisher(ctrl)
	mValidator := NewMocknipostValidator(ctrl)

	handler := NewMalfeasanceHandlerV2(
		logger,
		mPublish,
		edVerifier,
		mValidator,
	)

	return &testMalHandler{
		MalfeasanceHandlerV2: handler,

		observedLogs: observedLogs,
		ctrl:         ctrl,
		mPublish:     mPublish,
		mValidator:   mValidator,
	}
}

func TestRegister(t *testing.T) {
	t.Parallel()

	t.Run("register", func(t *testing.T) {
		t.Parallel()
		th := newTestMalHandler(t)
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)

		th.Register(sig)

		require.Equal(t, 1, th.observedLogs.Len())
		require.Equal(t, zap.DebugLevel, th.observedLogs.All()[0].Level)
		require.Contains(t, th.observedLogs.All()[0].Message, "registered signing key")
	})

	t.Run("already registered", func(t *testing.T) {
		t.Parallel()
		th := newTestMalHandler(t)
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)

		th.Register(sig)
		th.Register(sig)

		logs := th.observedLogs.FilterLevelExact(zap.ErrorLevel)

		require.Equal(t, 1, logs.Len())
		require.Equal(t, zap.ErrorLevel, logs.All()[0].Level)
		require.Contains(t, logs.All()[0].Message, "signing key already registered")
	})
}

func TestHandler_Info(t *testing.T) {
	t.Parallel()

	t.Run("decode proof error", func(t *testing.T) {
		t.Parallel()
		th := newTestMalHandler(t)

		info, err := th.Info([]byte("invalid proof"))
		require.Error(t, err)
		require.Contains(t, err.Error(), "decoding ATX malfeasance proof")
		require.Nil(t, info)
	})

	tt := []struct {
		name      string
		proofType wire.ProofType
		proof     wire.Proof
	}{
		{
			name:      "double marry proof",
			proofType: wire.DoubleMarry,
			proof:     &wire.ProofDoubleMarry{},
		},
		{
			name:      "double merge proof",
			proofType: wire.DoubleMerge,
			proof:     &wire.ProofDoubleMerge{},
		},
		{
			name:      "invalid post",
			proofType: wire.InvalidPost,
			proof:     &wire.ProofInvalidPost{},
		},
		{
			name:      "invalid prev atx v1",
			proofType: wire.InvalidPreviousV1,
			proof:     &wire.ProofInvalidPrevAtxV1{},
		},
		{
			name:      "invalid prev atx v2",
			proofType: wire.InvalidPreviousV2,
			proof:     &wire.ProofInvalidPrevAtxV2{},
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			th := newTestMalHandler(t)

			atxProof := &wire.ATXProof{
				Version: wire.ProofVersion(1),

				ProofType: tc.proofType,
				Proof:     codec.MustEncode(tc.proof),
			}
			data, err := codec.Encode(atxProof)
			require.NoError(t, err)

			expectedInfo := tc.proof.Info()
			expectedInfo["type"] = tc.proof.String()

			info, err := th.Info(data)
			require.NoError(t, err)
			require.Equal(t, expectedInfo, info)
		})
	}
}

func TestPublish(t *testing.T) {
	t.Parallel()

	t.Run("valid proof", func(t *testing.T) {
		t.Parallel()

		th := newTestMalHandler(t)

		nodeID := types.RandomNodeID()
		proof := wire.NewMockProof(th.ctrl)

		proof.EXPECT().Valid(context.Background(), th.MalfeasanceHandlerV2).Return(nodeID, nil)
		proof.EXPECT().Type().Return(wire.DoubleMarry)
		proof.EXPECT().EncodeScale(gomock.Any())

		atxProof := &wire.ATXProof{
			Version:   0x01, // for now we only have one version
			ProofType: wire.DoubleMarry,

			Proof: []byte{},
		}
		th.mPublish.EXPECT().PublishATXProof(context.Background(), nodeID, codec.MustEncode(atxProof)).Return(nil)

		err := th.Publish(context.Background(), nodeID, proof)
		require.NoError(t, err)
	})

	t.Run("invalid proof", func(t *testing.T) {
		t.Parallel()

		th := newTestMalHandler(t)

		proof := wire.NewMockProof(th.ctrl)
		nodeID := types.RandomNodeID()
		errInvalidProof := errors.New("invalid proof")
		proof.EXPECT().Valid(context.Background(), th.MalfeasanceHandlerV2).Return(types.EmptyNodeID, errInvalidProof)

		err := th.Publish(context.Background(), nodeID, proof)
		require.ErrorIs(t, err, errInvalidProof)
		require.ErrorContains(t, err, "proof not valid")
	})

	t.Run("proof for self", func(t *testing.T) {
		t.Parallel()

		th := newTestMalHandler(t)

		sig1, err := signing.NewEdSigner()
		require.NoError(t, err)
		th.Register(sig1)

		proof := wire.NewMockProof(th.ctrl)

		err = th.Publish(context.Background(), sig1.NodeID(), proof)
		require.ErrorContains(t, err, fmt.Sprintf("identity %s is managed by node", sig1.NodeID()))
	})

	t.Run("proof for different nodeID", func(t *testing.T) {
		t.Parallel()

		th := newTestMalHandler(t)

		sig1 := types.RandomNodeID()
		sig2 := types.RandomNodeID()

		proof := wire.NewMockProof(th.ctrl)
		proof.EXPECT().Valid(context.Background(), th.MalfeasanceHandlerV2).Return(sig2, nil)

		err := th.Publish(context.Background(), sig1, proof)
		require.ErrorContains(t, err,
			fmt.Sprintf("proof for %s does not match node ID %s", sig2.ShortString(), sig1.ShortString()),
		)
	})
}

func TestValidate(t *testing.T) {
	t.Parallel()

	t.Run("proof fails decoding", func(t *testing.T) {
		t.Parallel()

		th := newTestMalHandler(t)

		id, err := th.Validate(context.Background(), []byte{})
		require.ErrorContains(t, err, "decoding ATX malfeasance proof")
		require.Equal(t, types.EmptyNodeID, id)
	})

	t.Run("unknown proof type", func(t *testing.T) {
		t.Parallel()

		th := newTestMalHandler(t)

		atxProof := &wire.ATXProof{
			Version:   0x01,
			ProofType: 0x42, // unknown proof type
		}

		id, err := th.Validate(context.Background(), codec.MustEncode(atxProof))
		require.ErrorContains(t, err, "unknown ATX malfeasance proof type")
		require.Equal(t, types.EmptyNodeID, id)
	})

	t.Run("atx proof fails decoding", func(t *testing.T) {
		t.Parallel()

		th := newTestMalHandler(t)

		atxProof := &wire.ATXProof{
			Version:   0x01,
			ProofType: wire.DoubleMarry,
			Proof:     []byte{}, // invalid proof
		}

		id, err := th.Validate(context.Background(), codec.MustEncode(atxProof))
		require.ErrorContains(t, err, "decoding ATX malfeasance proof of type 0x11")
		require.Equal(t, types.EmptyNodeID, id)
	})

	genProof := func(t *testing.T, sig *signing.EdSigner) *wire.ProofInvalidPost {
		db := statesql.InMemoryTest(t)

		nipostChallenge := types.RandomHash()
		const numUnits = uint32(11)
		post := wire.PostV1{
			Nonce:   rand.Uint32(),
			Indices: types.RandomBytes(11),
			Pow:     rand.Uint64(),
		}
		atx := wire.NewTestActivationTxV2(
			wire.WithNIPost(
				wire.WithNIPostChallenge(nipostChallenge),
				wire.WithNIPostSubPost(wire.SubPostV2{
					Post:     post,
					NumUnits: numUnits,
				}),
			),
		)
		atx.Sign(sig)
		commitmentATX := types.RandomATXID()

		const invalidPostIdx = 7
		const validPostIdx = 15
		proof, err := wire.NewInvalidPostProof(db, atx, commitmentATX, sig.NodeID(), 0, invalidPostIdx, validPostIdx)
		require.NoError(t, err)
		return proof
	}

	t.Run("valid proof", func(t *testing.T) {
		t.Parallel()

		th := newTestMalHandler(t)
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		proof := genProof(t, sig)

		atxProof := &wire.ATXProof{
			Version:   0x01, // for now we only have one version
			ProofType: proof.Type(),
			Proof:     codec.MustEncode(proof),
		}

		th.mValidator.EXPECT().PostV2(
			context.Background(),
			proof.NodeID,
			proof.InvalidPostProof.CommitmentATX,
			wire.PostFromWireV1(&proof.InvalidPostProof.Post),
			proof.InvalidPostProof.Challenge.Bytes(),
			proof.InvalidPostProof.NumUnits,
			gomock.Cond(func(opt validatorOption) bool {
				opts := &validatorOptions{}
				opt(opts)
				return *opts.postIdx == int(proof.InvalidPostProof.InvalidPostIndex)
			}),
		).Return(errors.New("invalid post"))
		th.mValidator.EXPECT().PostV2(
			context.Background(),
			proof.NodeID,
			proof.InvalidPostProof.CommitmentATX,
			wire.PostFromWireV1(&proof.InvalidPostProof.Post),
			proof.InvalidPostProof.Challenge.Bytes(),
			proof.InvalidPostProof.NumUnits,
			gomock.Cond(func(opt validatorOption) bool {
				opts := &validatorOptions{}
				opt(opts)
				return *opts.postIdx == int(proof.InvalidPostProof.ValidPostIndex)
			}),
		).Return(nil)
		id, err := th.Validate(context.Background(), codec.MustEncode(atxProof))
		require.NoError(t, err)
		require.Equal(t, sig.NodeID(), id)
	})

	t.Run("invalid proof", func(t *testing.T) {
		t.Parallel()

		th := newTestMalHandler(t)
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		proof := genProof(t, sig)
		proof.NodeID = types.RandomNodeID()

		atxProof := &wire.ATXProof{
			Version:   0x01, // for now we only have one version
			ProofType: proof.Type(),
			Proof:     codec.MustEncode(proof),
		}

		id, err := th.Validate(context.Background(), codec.MustEncode(atxProof))
		require.ErrorContains(t, err, "validating ATX malfeasance proof:")
		require.Equal(t, types.EmptyNodeID, id)
	})
}
