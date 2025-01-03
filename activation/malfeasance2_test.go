package activation

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/codec"
)

type testMalHandler struct {
	*MalfeasanceHandlerV2
}

func newTestMalHandler(tb testing.TB) *testMalHandler {
	handler := NewMalfeasanceHandlerV2()

	return &testMalHandler{
		MalfeasanceHandlerV2: handler,
	}
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
