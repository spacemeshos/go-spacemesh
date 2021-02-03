package pendingtxs

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/stretchr/testify/require"
	"testing"
)

var alice = signing.NewEdSignerSeed("alice")

func newTx(t *testing.T, nonce, totalAmount, fee uint64) types.Transaction {
	tx, err := types.OldCoinTx{
		AccountNonce: nonce,
		Recipient:    types.Address{},
		Amount:       totalAmount - fee,
		GasLimit:     3,
		Fee:          fee,
	}.NewEd().Sign(alice)
	require.NoError(t, err)
	return tx
}

func TestNewAccountPendingTxs(t *testing.T) {
	r := require.New(t)

	pendingTxs := NewAccountPendingTxs()
	prevNonce := uint64(5)
	prevBalance := uint64(1000)

	// Empty list should project same as input
	empty := pendingTxs.IsEmpty()
	r.True(empty)
	nonce, balance := pendingTxs.GetProjection(prevNonce, prevBalance)
	r.Equal(prevNonce, nonce)
	r.Equal(prevBalance, balance)

	// Adding works
	pendingTxs.Add(1, newTx(t, 5, 100, 1))
	empty = pendingTxs.IsEmpty()
	r.False(empty)
	nonce, balance = pendingTxs.GetProjection(prevNonce, prevBalance)
	r.Equal(prevNonce+1, nonce)
	r.Equal(int(prevBalance)-100, int(balance))

	// Accepting a transaction removes all same-nonce transactions
	pendingTxs.RemoveAccepted([]types.Transaction{
		newTx(t, 5, 50, 1),
	})
	empty = pendingTxs.IsEmpty()
	r.True(empty)
	nonce, balance = pendingTxs.GetProjection(prevNonce, prevBalance)
	r.Equal(prevNonce, nonce)
	r.Equal(prevBalance, balance)

	// Add a transaction again
	pendingTxs.Add(1, newTx(t, 5, 100, 1))
	nonce, balance = pendingTxs.GetProjection(prevNonce, prevBalance)
	r.Equal(prevNonce+1, nonce)
	r.Equal(prevBalance-100, balance)

	// Now add it again in a higher layer -- this layer is now required when rejecting
	pendingTxs.Add(2, newTx(t, 5, 100, 1))
	nonce, balance = pendingTxs.GetProjection(prevNonce, prevBalance)
	r.Equal(prevNonce+1, nonce)
	r.Equal(prevBalance-100, balance)

	// When it's added again in a lower layer -- nothing changes
	pendingTxs.Add(0, newTx(t, 5, 100, 1))
	nonce, balance = pendingTxs.GetProjection(prevNonce, prevBalance)
	r.Equal(prevNonce+1, nonce)
	r.Equal(prevBalance-100, balance)

	// Rejecting a transaction with same-nonce does NOT remove a different transactions
	pendingTxs.RemoveRejected([]types.Transaction{
		newTx(t, 5, 50, 1),
	}, 2)
	nonce, balance = pendingTxs.GetProjection(prevNonce, prevBalance)
	r.Equal(prevNonce+1, nonce)
	r.Equal(prevBalance-100, balance)

	// Rejecting a transaction in a lower layer than the highest seen for it, does not remove it, either
	pendingTxs.RemoveRejected([]types.Transaction{
		newTx(t, 5, 100, 1),
	}, 1)
	nonce, balance = pendingTxs.GetProjection(prevNonce, prevBalance)
	r.Equal(prevNonce+1, nonce)
	r.Equal(prevBalance-100, balance)

	// However, rejecting a transaction in the highest layer it was seen in DOES remove it
	pendingTxs.RemoveRejected([]types.Transaction{
		newTx(t, 5, 100, 1),
	}, 2)
	nonce, balance = pendingTxs.GetProjection(prevNonce, prevBalance)
	r.Equal(prevNonce, nonce)
	r.Equal(prevBalance, balance)

	// When several transactions exist with same nonce, projection is pessimistic (reduces balance by higher amount)
	pendingTxs.Add(1,
		newTx(t, 5, 100, 2),
		newTx(t, 5, 50, 1),
	)
	nonce, balance = pendingTxs.GetProjection(prevNonce, prevBalance)
	r.Equal(prevNonce+1, nonce)
	r.Equal(int(prevBalance)-100, int(balance))

	// Adding higher nonce transactions with a missing nonce in between does not affect projection
	pendingTxs.Add(1,
		newTx(t, 7, 100, 1),
		newTx(t, 8, 50, 1),
	)
	nonce, balance = pendingTxs.GetProjection(prevNonce, prevBalance)
	r.Equal(prevNonce+1, nonce)
	r.Equal(prevBalance-100, balance)

	// Trying to fill the gap with a transaction that would over-draft the account has no effect
	pendingTxs.Add(1, newTx(t, 6, 950, 2)) // to prefer this transaction against another
	nonce, balance = pendingTxs.GetProjection(prevNonce, prevBalance)
	r.Equal(prevNonce+1, nonce)
	r.Equal(prevBalance-100, balance)

	// But filling the gap with a valid transaction causes the higher nonce transactions to start being considered
	pendingTxs.Add(1, newTx(t, 6, 50, 1))
	nonce, balance = pendingTxs.GetProjection(prevNonce, prevBalance)
	r.Equal(prevNonce+4, nonce)
	r.Equal(prevBalance-100-50-100-50, balance)

	// Rejecting a transaction only removes that version, if several exist
	// This can also cause a transaction that would previously over-draft the account to become valid
	pendingTxs.RemoveRejected([]types.Transaction{
		newTx(t, 5, 100, 2),
	}, 2)
	nonce, balance = pendingTxs.GetProjection(prevNonce, prevBalance)
	r.Equal(prevNonce+2, nonce)
	r.Equal(prevBalance-50-950, balance)
}
