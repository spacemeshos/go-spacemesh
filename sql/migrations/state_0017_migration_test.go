package migrations

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/common/fixture"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
)

type addAtxOpt func(atx *wire.ActivationTxV1)

func withNonce(nonce uint64) addAtxOpt {
	return func(atx *wire.ActivationTxV1) {
		atx.VRFNonce = &nonce
	}
}

func withPrevATXID(atxID types.ATXID) addAtxOpt {
	return func(atx *wire.ActivationTxV1) {
		atx.PrevATXID = atxID
	}
}

func addAtx(
	t *testing.T,
	db sql.Executor,
	signer *signing.EdSigner,
	epoch types.EpochID,
	sequence uint64,
	opts ...addAtxOpt,
) types.ATXID {
	watx := &wire.ActivationTxV1{
		InnerActivationTxV1: wire.InnerActivationTxV1{
			NIPostChallengeV1: wire.NIPostChallengeV1{
				PublishEpoch: epoch,
				Sequence:     sequence,
			},
			Coinbase: types.Address{1, 2, 3},
			NumUnits: 2,
		},
	}
	for _, opt := range opts {
		opt(watx)
	}
	watx.Sign(signer)

	atx := fixture.ToAtx(t, watx)
	require.NoError(t, atxs.AddMaybeNoNonce(db, atx))
	return atx.ID()
}

func Test0017Migration(t *testing.T) {
	for i := 0; i < 10; i++ {
		db := sql.InMemory()
		err := db.WithTx(context.Background(), func(tx *sql.Tx) error {
			var sigs [3]*signing.EdSigner
			for n := range sigs {
				var err error
				sigs[n], err = signing.NewEdSigner()
				require.NoError(t, err)
			}
			id11 := addAtx(t, tx, sigs[0], 1, 0, withNonce(42))
			id12 := addAtx(t, tx, sigs[1], 1, 0, withNonce(123))
			id13 := addAtx(t, tx, sigs[2], 1, 0, withNonce(456))
			id14 := addAtx(t, tx, sigs[2], 1, 0, withPrevATXID(id13)) // equivocation
			id21 := addAtx(t, tx, sigs[0], 2, 1, withPrevATXID(id11))
			id22 := addAtx(t, tx, sigs[1], 2, 1, withPrevATXID(id12))
			id23 := addAtx(t, tx, sigs[2], 2, 1, withPrevATXID(id13))
			id24 := addAtx(t, tx, sigs[2], 2, 2, withPrevATXID(id13)) // equivocation
			id25 := addAtx(t, tx, sigs[2], 2, 3, withPrevATXID(id14)) // equivocation
			id31 := addAtx(t, tx, sigs[0], 3, 2, withNonce(111), withPrevATXID(id11))
			id32 := addAtx(t, tx, sigs[1], 3, 2, withPrevATXID(id12)) // jump back over an epoch
			id33 := addAtx(t, tx, sigs[2], 3, 4, withPrevATXID(id25))
			m := New0017Migration(zaptest.NewLogger(t))
			require.Equal(t, 17, m.Order())
			err := m.Apply(tx)
			require.NoError(t, err)
			for n, v := range []struct {
				id    types.ATXID
				nonce types.VRFPostIndex
			}{
				{id: id11, nonce: 42},
				{id: id12, nonce: 123},
				{id: id13, nonce: 456},
				{id: id14, nonce: 456},
				{id: id21, nonce: 42},
				{id: id22, nonce: 123},
				{id: id23, nonce: 456},
				{id: id24, nonce: 456},
				{id: id25, nonce: 456},
				{id: id31, nonce: 111},
				{id: id32, nonce: 123},
				{id: id33, nonce: 456},
			} {
				nonce, err := atxs.NonceByID(tx, v.id)
				require.NoError(t, err, "%d: getting nonce for ATX ID %s", n, v.id)
				require.Equal(t, v.nonce, nonce, "%d: nonce for ATX ID %s", n, v.id)
			}
			return nil
		})
		require.NoError(t, err)
	}
}
