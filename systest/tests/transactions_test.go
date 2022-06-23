package tests

import (
	"testing"

	spacemeshv1 "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/systest/cluster"
	"github.com/spacemeshos/go-spacemesh/systest/testcontext"
)

func testTransactions(t *testing.T, tctx *testcontext.Context, cl *cluster.Cluster) {
	var (
		// start sending transactions after two layers or after genesis
		first              = maxLayer(currentLayer(tctx, t, cl.Client(0))+2, 8)
		sendFor     uint32 = 8
		stopSending        = first + sendFor
		stopWaiting        = stopSending + 4
		batch              = 3
		amount             = 100
	)
	tctx.Log.Debugw("running transactions test",
		"from", first,
		"stop sending", stopSending,
		"stop waiting", stopWaiting,
	)
	receiver := [20]byte{11, 1, 1}
	state := spacemeshv1.NewGlobalStateServiceClient(cl.Client(0))
	response, err := state.Account(tctx, &spacemeshv1.AccountRequest{AccountId: &spacemeshv1.AccountId{Address: receiver[:]}})
	require.NoError(t, err)
	before := response.AccountWrapper.StateCurrent.Balance

	eg, ctx := errgroup.WithContext(tctx)
	for i := 0; i < cl.Accounts(); i++ {
		client := cl.Client(i % cl.Total())
		nonce, err := getNonce(tctx, client, cl.Address(i))
		require.NoError(t, err)
		submitter := newTransactionSubmitter(cl.Private(i), receiver, uint64(amount), nonce, client)
		watchLayers(ctx, eg, client, func(layer *spacemeshv1.LayerStreamResponse) (bool, error) {
			if layer.Layer.Number.Number == stopSending {
				return false, nil
			}
			if layer.Layer.Status != spacemeshv1.Layer_LAYER_STATUS_APPROVED ||
				layer.Layer.Number.Number < first {
				return true, nil
			}
			tctx.Log.Debugw("submitting transactions",
				"layer", layer.Layer.Number.Number,
				"client", client.Name,
				"batch", batch,
			)
			for j := 0; j < batch; j++ {
				if err := submitter(ctx); err != nil {
					return false, err
				}
			}
			return true, nil
		})
	}
	txs := make([][]*spacemeshv1.Transaction, cl.Total())
	for i := 0; i < cl.Total(); i++ {
		i := i
		client := cl.Client(i)
		watchLayers(ctx, eg, client, func(layer *spacemeshv1.LayerStreamResponse) (bool, error) {
			if layer.Layer.Number.Number == stopWaiting {
				return false, nil
			}
			if layer.Layer.Status != spacemeshv1.Layer_LAYER_STATUS_CONFIRMED {
				return true, nil
			}
			addtxs := []*spacemeshv1.Transaction{}
			for _, block := range layer.Layer.Blocks {
				addtxs = append(addtxs, block.Transactions...)
			}
			tctx.Log.Debugw("received transactions",
				"layer", layer.Layer.Number.Number,
				"client", client.Name,
				"blocks", len(layer.Layer.Blocks),
				"transactions", len(addtxs),
			)
			txs[i] = append(txs[i], addtxs...)
			return true, nil
		})
	}

	require.NoError(t, eg.Wait())
	reference := txs[0]
	for _, tested := range txs[1:] {
		require.Len(t, tested, len(reference))
		for i := range reference {
			require.Equal(t, reference[i], tested[i])
		}
	}

	diff := batch * amount * int(sendFor) * cl.Accounts()
	for i := 0; i < cl.Total(); i++ {
		client := cl.Client(i)
		state := spacemeshv1.NewGlobalStateServiceClient(client)
		response, err := state.Account(tctx, &spacemeshv1.AccountRequest{AccountId: &spacemeshv1.AccountId{Address: receiver[:]}})
		require.NoError(t, err)
		after := response.AccountWrapper.StateCurrent.Balance
		tctx.Log.Debugw("receiver state",
			"before", before.Value,
			"after", after.Value,
			"expected-diff", diff,
			"diff", after.Value-before.Value,
		)
		require.Equal(t, int(before.Value)+diff,
			int(response.AccountWrapper.StateCurrent.Balance.Value), "client=%s", client.Name)
	}
}
