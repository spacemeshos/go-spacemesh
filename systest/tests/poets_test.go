package tests

import (
	"os"
	"strconv"
	"testing"

	spacemeshv1 "github.com/spacemeshos/api/release/go/spacemesh/v1"
	v1 "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/systest/cluster"
	"github.com/spacemeshos/go-spacemesh/systest/testcontext"
)

func TestPoetsFailures(t *testing.T) {
	t.Parallel()
	tctx := testcontext.New(t, testcontext.Labels("sanity"))
	tctx.Log.Debug("TestPoetsFailures start")

	cl, err := cluster.Reuse(tctx, cluster.WithKeys(10))
	require.NoError(t, err)
	tctx.Log.Debug("Obtained cluster")

	testPoetDies(t, tctx, cl)
	tctx.Log.Debug("TestPoetsFailures is done")
}

func testPoetDies(t *testing.T, tctx *testcontext.Context, cl *cluster.Cluster) {
	layersCount := uint32(20)
	if value, err := strconv.ParseUint(os.Getenv("SYSTEST_LAYERS_TO_CHECK"), 10, 32); err == nil {
		layersCount = uint32(value)
	}
	const epochSize = 4
	first := nextFirstLayer(currentLayer(tctx, t, cl.Client(0)), epochSize)
	last := first + layersCount - 1
	tctx.Log.Debugw("watching layers between", "first", first, "last", last)

	createdch := make(chan *spacemeshv1.Proposal, cl.Total()*(int(layersCount)))

	eg, ctx := errgroup.WithContext(tctx)
	for i := 0; i < cl.Total(); i++ {
		clientId := i
		client := cl.Client(clientId)
		tctx.Log.Debugw("watching", "client", client.Name, "clientId", clientId)
		watchProposals(ctx, eg, client, func(proposal *spacemeshv1.Proposal) (bool, error) {
			if proposal.Layer.Number < first {
				return true, nil
			}
			if proposal.Layer.Number > last {
				tctx.Log.Debug("proposal watcher is done")
				return false, nil
			}

			if proposal.Status == spacemeshv1.Proposal_Created {
				tctx.Log.Debugw("received proposal event",
					"client", client.Name,
					"layer", proposal.Layer.Number,
					"eligibilities", len(proposal.Eligibilities),
				)
				createdch <- proposal
			}
			return true, nil
		})
	}

	watchLayers(ctx, eg, cl.Client(0), func(layer *spacemeshv1.LayerStreamResponse) (bool, error) {
		// Will kill a poet from time to time
		if layer.Layer.Number.Number > last {
			tctx.Log.Debug("Poet killer is done")
			return false, nil
		}
		if layer.Layer.GetStatus() != v1.Layer_LAYER_STATUS_APPLIED {
			return true, nil
		}
		// don't kill a poet if this is not ~middle of epoch
		if ((layer.Layer.GetNumber().GetNumber() + epochSize/2) % epochSize) != 0 {
			return true, nil
		}

		poetToDelete := cl.Poet(0)
		tctx.Log.Debugw("deleting poet pod", "poet", poetToDelete.Name, "layer", layer.Layer.GetNumber().GetNumber())
		require.NoError(t, cl.DeletePoet(tctx, 0))
		require.NoError(t, cl.AddPoet(tctx))

		return true, nil
	})

	require.NoError(t, eg.Wait())
	close(createdch)

	created := map[uint32][]*spacemeshv1.Proposal{}
	beacons := map[uint64]map[string]struct{}{}
	for proposal := range createdch {
		created[proposal.Layer.Number] = append(created[proposal.Layer.Number], proposal)
		if edata := proposal.GetData(); edata != nil {
			if _, exist := beacons[proposal.Epoch.Value]; !exist {
				beacons[proposal.Epoch.Value] = map[string]struct{}{}
			}
			beacons[proposal.Epoch.Value][prettyHex(edata.Beacon)] = struct{}{}
		}
	}

	requireEqualEligibilities(t, created)

	for epoch := range beacons {
		assert.Len(t, beacons[epoch], 1, "epoch=%d", epoch)
	}
}
