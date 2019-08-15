package mesh

import (
	"github.com/spacemeshos/go-spacemesh/address"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestNewAccountPendingTxs(t *testing.T) {
	r := require.New(t)

	pendingTxs := newAccountPendingTxs()
	prevNonce := uint64(5)
	prevBalance := uint64(1000)

	// Empty list should project same as input
	empty := pendingTxs.IsEmpty()
	r.True(empty)
	nonce, balance := pendingTxs.GetProjection(prevNonce, prevBalance)
	r.Equal(prevNonce, nonce)
	r.Equal(prevBalance, balance)

	// Adding works
	pendingTxs.Add([]tinyTx{
		addressableTxToTiny(newTx(address.BytesToAddress(nil), 5, 100)),
	}, 1)
	empty = pendingTxs.IsEmpty()
	r.False(empty)
	nonce, balance = pendingTxs.GetProjection(prevNonce, prevBalance)
	r.Equal(prevNonce+1, nonce)
	r.Equal(prevBalance-100, balance)

	// Accepting a transaction removes all same-nonce transactions
	pendingTxs.Remove([]tinyTx{
		addressableTxToTiny(newTx(address.BytesToAddress(nil), 5, 50)),
	}, nil, 1)
	empty = pendingTxs.IsEmpty()
	r.True(empty)
	nonce, balance = pendingTxs.GetProjection(prevNonce, prevBalance)
	r.Equal(prevNonce, nonce)
	r.Equal(prevBalance, balance)

	// Add a transaction again
	pendingTxs.Add([]tinyTx{
		addressableTxToTiny(newTx(address.BytesToAddress(nil), 5, 100)),
	}, 1)
	nonce, balance = pendingTxs.GetProjection(prevNonce, prevBalance)
	r.Equal(prevNonce+1, nonce)
	r.Equal(prevBalance-100, balance)

	// Now add it again in a higher layer -- this layer is now required when rejecting
	pendingTxs.Add([]tinyTx{
		addressableTxToTiny(newTx(address.BytesToAddress(nil), 5, 100)),
	}, 2)
	nonce, balance = pendingTxs.GetProjection(prevNonce, prevBalance)
	r.Equal(prevNonce+1, nonce)
	r.Equal(prevBalance-100, balance)

	// When it's added again in a lower layer -- nothing changes
	pendingTxs.Add([]tinyTx{
		addressableTxToTiny(newTx(address.BytesToAddress(nil), 5, 100)),
	}, 0)
	nonce, balance = pendingTxs.GetProjection(prevNonce, prevBalance)
	r.Equal(prevNonce+1, nonce)
	r.Equal(prevBalance-100, balance)

	// Rejecting a transaction with same-nonce does NOT remove a different transactions
	pendingTxs.Remove(nil, []tinyTx{
		addressableTxToTiny(newTx(address.BytesToAddress(nil), 5, 50)),
	}, 2)
	nonce, balance = pendingTxs.GetProjection(prevNonce, prevBalance)
	r.Equal(prevNonce+1, nonce)
	r.Equal(prevBalance-100, balance)

	// Rejecting a transaction in a lower layer than the highest seen for it, does not remove it, either
	pendingTxs.Remove(nil, []tinyTx{
		addressableTxToTiny(newTx(address.BytesToAddress(nil), 5, 100)),
	}, 1)
	nonce, balance = pendingTxs.GetProjection(prevNonce, prevBalance)
	r.Equal(prevNonce+1, nonce)
	r.Equal(prevBalance-100, balance)

	// However, rejecting a transaction in the highest layer it was seen in DOES remove it
	pendingTxs.Remove(nil, []tinyTx{
		addressableTxToTiny(newTx(address.BytesToAddress(nil), 5, 100)),
	}, 2)
	nonce, balance = pendingTxs.GetProjection(prevNonce, prevBalance)
	r.Equal(prevNonce, nonce)
	r.Equal(prevBalance, balance)

	// When several transactions exist with same nonce, projection is pessimistic (reduces balance by higher amount)
	pendingTxs.Add([]tinyTx{
		addressableTxToTiny(newTx(address.BytesToAddress(nil), 5, 100)),
		addressableTxToTiny(newTx(address.BytesToAddress(nil), 5, 50)),
	}, 1)
	nonce, balance = pendingTxs.GetProjection(prevNonce, prevBalance)
	r.Equal(prevNonce+1, nonce)
	r.Equal(prevBalance-100, balance)

	// Adding higher nonce transactions with a missing nonce in between does not affect projection
	pendingTxs.Add([]tinyTx{
		addressableTxToTiny(newTx(address.BytesToAddress(nil), 7, 100)),
		addressableTxToTiny(newTx(address.BytesToAddress(nil), 8, 50)),
	}, 1)
	nonce, balance = pendingTxs.GetProjection(prevNonce, prevBalance)
	r.Equal(prevNonce+1, nonce)
	r.Equal(prevBalance-100, balance)

	// Trying to fill the gap with a transaction that would over-draft the account has no effect
	pendingTxs.Add([]tinyTx{
		addressableTxToTiny(newTx(address.BytesToAddress(nil), 6, 950)),
	}, 1)
	nonce, balance = pendingTxs.GetProjection(prevNonce, prevBalance)
	r.Equal(prevNonce+1, nonce)
	r.Equal(prevBalance-100, balance)

	// But filling the gap with a valid transaction causes the higher nonce transactions to start being considered
	pendingTxs.Add([]tinyTx{
		addressableTxToTiny(newTx(address.BytesToAddress(nil), 6, 50)),
	}, 1)
	nonce, balance = pendingTxs.GetProjection(prevNonce, prevBalance)
	r.Equal(prevNonce+4, nonce)
	r.Equal(prevBalance-100-50-100-50, balance)
}
