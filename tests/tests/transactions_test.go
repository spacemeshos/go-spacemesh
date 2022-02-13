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
	const (
		keys        = 10
		genesis     = 7
		sendFor     = 8
		stopSending = genesis + sendFor + 1
		stopWaiting = stopSending + 2
		batch       = 5
		amount      = 100
	)
	receiver := [20]byte{11, 1, 1}

	eg, ctx := errgroup.WithContext(tctx)
	for i := 0; i < keys; i++ {
		client := cl.Client(i % cl.Total())
		submitter := newTransactionSubmitter(cl.Private(i), receiver, amount, client)
		collectLayers(ctx, eg, client, func(layer *spacemeshv1.LayerStreamResponse) (bool, error) {
			if layer.Layer.Number.Number == stopSending {
				return false, nil
			}
			if layer.Layer.Status != spacemeshv1.Layer_LAYER_STATUS_APPROVED {
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
		collectLayers(ctx, eg, client, func(layer *spacemeshv1.LayerStreamResponse) (bool, error) {
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
	for i := 0; i < cl.Total(); i++ {
		client := cl.Client(i)
		state := spacemeshv1.NewGlobalStateServiceClient(client)
		response, err := state.Account(tctx, &spacemeshv1.AccountRequest{AccountId: &spacemeshv1.AccountId{Address: receiver[:]}})
		require.NoError(t, err)
		require.Equal(t, batch*amount*sendFor*keys,
			int(response.AccountWrapper.StateCurrent.Balance.Value), "client=%s", client.Name)
	}
}
