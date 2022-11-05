package tests

import (
	"context"
	"fmt"
	"testing"

	spacemeshv1 "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/systest/chaos"
	"github.com/spacemeshos/go-spacemesh/systest/cluster"
	"github.com/spacemeshos/go-spacemesh/systest/testcontext"
)

func sendTransactions(tctx *testcontext.Context, cl *cluster.Cluster, stop uint32) *errgroup.Group {
	var (
		eg, ctx  = errgroup.WithContext(tctx)
		first    = uint32(layersPerEpoch * 2)
		receiver = types.GenerateAddress([]byte{11, 1, 1})
		amount   = 100
		batch    = 10
	)
	for i := 0; i < cl.Accounts(); i++ {
		i := i
		client := cl.Client(i % cl.Total())
		watchLayers(ctx, eg, client, func(layer *spacemeshv1.LayerStreamResponse) (bool, error) {
			if layer.Layer.Number.Number >= stop {
				return false, nil
			}
			if layer.Layer.Number.Number < first {
				return true, nil
			}
			nonce, err := getNonce(tctx, client, cl.Address(i))
			if err != nil {
				tctx.Log.Debugw("failed to get nonce", "client", client.Name, "err", err.Error())
				return false, fmt.Errorf("get nonce failed (%s:%s): %w", client.Name, cl.Address(i), err)
			}
			if nonce == 0 {
				if err := submitSpawn(ctx, cl, i, client); err != nil {
					tctx.Log.Debugw("failed to spawn", "client", client.Name, "err", err.Error())
					return false, fmt.Errorf("spawn failed (%s:%s): %w", client.Name, cl.Address(i), err)
				}
				return true, nil
			}

			if layer.Layer.Number.Number > first+1 {
				for j := 0; j < batch; j++ {
					if err := submitSpend(ctx, cl, i, receiver, uint64(amount), nonce+uint64(j), client); err != nil {
						tctx.Log.Debugw("failed to send", "client", client.Name, "err", err.Error())
						return false, fmt.Errorf("spend failed (%s:%s): %w", client.Name, cl.Address(i), err)
					}
				}
			}
			return true, nil
		})
	}
	return eg
}

func testPartition(t *testing.T, tctx *testcontext.Context, cl *cluster.Cluster, pct int, wait uint32) {
	require.Greater(t, cl.Bootnodes(), 1)

	var (
		startSplit = uint32(4*layersPerEpoch) - 1
		rejoin     = startSplit + 2*layersPerEpoch
		stop       = rejoin + wait*layersPerEpoch
	)

	tctx.Log.Debug("scheduling chaos...")
	eg, ctx := errgroup.WithContext(tctx)
	// make sure the first boot node is in the 2nd partition so the poet proof can be broadcast to both splits
	split := pct*cl.Total()/100 + 1
	scheduleChaos(ctx, eg, cl.Client(0), startSplit, rejoin, func(ctx context.Context) (chaos.Teardown, error) {
		var (
			left  []string
			right = []string{cl.Client(0).Name}
		)
		for i := 1; i < cl.Total(); i++ {
			if i < split {
				left = append(left, cl.Client(i).Name)
			} else {
				right = append(right, cl.Client(i).Name)
			}
		}
		tctx.Log.Debugw("long partition",
			"percentage", pct,
			"split", startSplit,
			"rejoin", rejoin,
			"left", left,
			"right", right,
		)
		return chaos.Partition2(tctx, fmt.Sprintf("split-%v-%v", pct, 100-pct), left, right)
	})

	// start sending transactions
	tctx.Log.Debug("sending transactions...")
	txeg := sendTransactions(tctx, cl, stop)

	type stateUpdate struct {
		layer  uint32
		hash   types.Hash32
		client string
	}
	numLayers := stop - types.GetEffectiveGenesis().Uint32()
	// assuming each client can update state for the same layer up to 10 times
	stateCh := make(chan *stateUpdate, uint32(cl.Total())*numLayers*10)
	tctx.Log.Debug("listening to state hashes...")
	for i := 0; i < cl.Total(); i++ {
		i := i
		client := cl.Client(i)
		watchStateHashes(ctx, eg, client, func(state *spacemeshv1.GlobalStateStreamResponse) (bool, error) {
			data := state.Datum.Datum
			require.IsType(t, &spacemeshv1.GlobalStateData_GlobalState{}, data)

			resp := data.(*spacemeshv1.GlobalStateData_GlobalState)
			layer := resp.GlobalState.Layer.Number
			if layer > stop {
				return false, nil
			}

			stateHash := types.BytesToHash(resp.GlobalState.RootHash)
			tctx.Log.Debugw("state hash collected",
				"client", client.Name,
				"layer", layer,
				"state", stateHash.ShortString())
			stateCh <- &stateUpdate{
				layer:  layer,
				hash:   stateHash,
				client: client.Name,
			}
			return true, nil
		})
	}

	finalErr := eg.Wait()
	close(stateCh)
	// this map contains info for debugging only
	hashes := make(map[uint32]map[types.Hash32][]string)
	// this map contains the latest hash reported by each client/layer
	latestStates := make(map[string]map[uint32]types.Hash32)
	for update := range stateCh {
		if _, ok := hashes[update.layer]; !ok {
			hashes[update.layer] = make(map[types.Hash32][]string)
		}
		if _, ok := hashes[update.layer][update.hash]; !ok {
			hashes[update.layer][update.hash] = []string{}
		}
		hashes[update.layer][update.hash] = append(hashes[update.layer][update.hash], update.client)

		if _, ok := latestStates[update.client]; !ok {
			latestStates[update.client] = make(map[uint32]types.Hash32)
		}
		latestStates[update.client][update.layer] = update.hash
	}
	for layer := uint32(layersPerEpoch * 2); layer <= stop; layer++ {
		tctx.Log.Debugw("client states",
			"layer", layer,
			"num_states", len(hashes[layer]),
			"states", log.ObjectMarshallerFunc(func(encoder log.ObjectEncoder) error {
				for hash, clients := range hashes[layer] {
					encoder.AddString("hash", hash.ShortString())
					encoder.AddInt("num_clients", len(clients))
					encoder.AddArray("clients", log.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
						for _, c := range clients {
							encoder.AppendString(c)
						}
						return nil
					}))
				}
				return nil
			}))
	}
	refState := latestStates[cl.Client(0).Name]
	pass := true
	for i := 1; i < cl.Total(); i++ {
		clientState := latestStates[cl.Client(i).Name]
		agree := true
		for layer := uint32(layersPerEpoch * 2); layer <= stop; layer++ {
			if clientState[layer] != refState[layer] {
				tctx.Log.Errorw("client state differs from ref state",
					"client", cl.Client(i).Name,
					"ref_client", cl.Client(0).Name,
					"layer", layer,
					"client_hash", clientState[layer],
					"ref_hash", refState[layer])
				agree = false
				break
			}
		}
		if agree {
			tctx.Log.Debugw("client agreed with ref client on all layers",
				"client", cl.Client(i).Name,
				"ref_client", cl.Client(0).Name)
		}
		pass = pass && agree
	}
	if finalErr != nil {
		tctx.Log.Errorw("test failed", "err", finalErr.Error())
	}
	require.NoError(t, finalErr)
	require.True(t, pass)
	_ = txeg.Wait()
}

func TestPartition_30_70(t *testing.T) {
	t.Parallel()

	tctx := testcontext.New(t, testcontext.Labels("destructive"))
	cl, err := cluster.Reuse(tctx, cluster.WithKeys(10))
	require.NoError(t, err)
	testPartition(t, tctx, cl, 30, 2)
}

func TestPartition_50_50(t *testing.T) {
	t.Parallel()

	tctx := testcontext.New(t, testcontext.Labels("destructive"))
	cl, err := cluster.Reuse(tctx, cluster.WithKeys(10))
	require.NoError(t, err)
	testPartition(t, tctx, cl, 50, 3)
}
