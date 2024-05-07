package migrations

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
)

type addAtxOpt func(atx *types.ActivationTx)

func withNonce(nonce types.VRFPostIndex) addAtxOpt {
	return func(atx *types.ActivationTx) {
		atx.VRFNonce = &nonce
	}
}

func withPrevATXID(atxID types.ATXID) addAtxOpt {
	return func(atx *types.ActivationTx) {
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
	atx := &types.ActivationTx{
		InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PublishEpoch: epoch,
				Sequence:     sequence,
				PrevATXID:    types.EmptyATXID,
			},
			Coinbase: types.Address{1, 2, 3},
			NumUnits: 2,
		},
	}
	for _, opt := range opts {
		opt(atx)
	}
	require.NoError(t, activation.SignAndFinalizeAtx(signer, atx))
	atx.SetEffectiveNumUnits(atx.NumUnits)
	atx.SetReceived(time.Now().Local())
	vAtx, err := atx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.AddMaybeNoNonce(db, vAtx))
	return vAtx.ID()
}

func Test_0017Migration_CompatibleSQL(t *testing.T) {
	file := filepath.Join(t.TempDir(), "test1.db")
	db, err := sql.Open("file:"+file,
		sql.WithMigration(New0017Migration(zaptest.NewLogger(t))),
	)
	require.NoError(t, err)

	var sqls1 []string
	_, err = db.Exec("SELECT sql FROM sqlite_schema;", nil, func(stmt *sql.Statement) bool {
		sql := stmt.ColumnText(0)
		sql = strings.Join(strings.Fields(sql), " ") // remove whitespace
		sqls1 = append(sqls1, sql)
		return true
	})
	require.NoError(t, err)
	require.NoError(t, db.Close())

	file = filepath.Join(t.TempDir(), "test2.db")
	db, err = sql.Open("file:" + file)
	require.NoError(t, err)

	var sqls2 []string
	_, err = db.Exec("SELECT sql FROM sqlite_schema;", nil, func(stmt *sql.Statement) bool {
		sql := stmt.ColumnText(0)
		sql = strings.Join(strings.Fields(sql), " ") // remove whitespace
		sqls2 = append(sqls2, sql)
		return true
	})
	require.NoError(t, err)
	require.NoError(t, db.Close())

	require.Equal(t, sqls1, sqls2)
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
			require.NoError(t, m.Apply(tx))
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
