package tests

import (
	"testing"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/systest/cluster"
	"github.com/spacemeshos/go-spacemesh/systest/parameters"
	"github.com/spacemeshos/go-spacemesh/systest/testcontext"
)

var layersToCheck = parameters.Int(
	"layers-to-check",
	"number of layers to check in TestPoetsFailures",
	20,
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
	layersCount := uint32(layersToCheck.Get(tctx.Parameters))
	first := nextFirstLayer(currentLayer(tctx, t, cl.Client(0)), layersPerEpoch)
	last := first + layersCount - 1
	tctx.Log.Debugw("watching layers between", "first", first, "last", last)

	createdch := make(chan *pb.Proposal, cl.Total()*(int(layersCount)))

	eg, ctx := errgroup.WithContext(tctx)
	for i := 0; i < cl.Total(); i++ {
		clientId := i
		client := cl.Client(clientId)
		tctx.Log.Debugw("watching", "client", client.Name, "clientId", clientId)
		watchProposals(ctx, eg, client, func(proposal *pb.Proposal) (bool, error) {
			if proposal.Layer.Number < first {
				return true, nil
			}
			if proposal.Layer.Number > last {
				tctx.Log.Debug("proposal watcher is done")
				return false, nil
			}

			if proposal.Status == pb.Proposal_Created {
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

	watchLayers(ctx, eg, cl.Client(0), func(layer *pb.LayerStreamResponse) (bool, error) {
		// Will kill a poet from time to time
		if layer.Layer.Number.Number > last {
			tctx.Log.Debug("Poet killer is done")
			return false, nil
		}
		if layer.Layer.GetStatus() != pb.Layer_LAYER_STATUS_APPLIED {
			return true, nil
		}
		// don't kill a poet if this is not ~middle of epoch
		if ((layer.Layer.GetNumber().GetNumber() + layersPerEpoch/2) % layersPerEpoch) != 0 {
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

	created := map[uint32][]*pb.Proposal{}
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
