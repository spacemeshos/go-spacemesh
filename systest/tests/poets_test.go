package tests

import (
	"encoding/hex"
	"fmt"
	"math"
	"testing"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
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
				tctx.Log.Desugar().Debug("received proposal event",
					zap.String("client", client.Name),
					zap.Uint32("layer", proposal.Layer.Number),
					zap.String("smesher", prettyHex(proposal.Smesher.Id)),
					zap.Int("eligibilities", len(proposal.Eligibilities)),
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
			tctx.Log.Desugar().Debug("new beacon",
				zap.String("smesher", prettyHex(proposal.Smesher.Id)),
				zap.String("beacon", prettyHex(edata.Beacon)),
				zap.Uint64("epoch", proposal.Epoch.Value),
			)
			beacons[proposal.Epoch.Value][prettyHex(edata.Beacon)] = struct{}{}
		}
	}

	requireEqualEligibilities(tctx, t, created)

	for epoch := range beacons {
		assert.Len(t, beacons[epoch], 1, "epoch=%d", epoch)
	}
}

func TestNodesUsingDifferentPoets(t *testing.T) {
	tctx := testcontext.New(t, testcontext.Labels("sanity"))
	if tctx.PoetSize < 2 {
		t.Skip("Skipping test for using different poets - test configured with less then 2 poets")
	}
	logger := tctx.Log.Named("TestNodesUsingDifferentPoets")

	cl, err := cluster.Reuse(tctx, cluster.WithKeys(10))
	require.NoError(t, err)
	logger.Debug("Obtained cluster")

	poetEndpoints := make([]string, 0, tctx.PoetSize)
	for i := 0; i < tctx.PoetSize; i++ {
		poetEndpoints = append(poetEndpoints, cluster.MakePoetEndpoint(i))
	}

	for i := 0; i < cl.Total(); i++ {
		node := cl.Client(i)
		endpoint := poetEndpoints[i%len(poetEndpoints)]
		logger.Debugw("updating node's poet server", "node", node.Name, "poet", endpoint)
		updated, err := updatePoetServers(tctx, node, []string{endpoint})
		require.NoError(t, err)
		require.True(t, updated)
	}

	layersCount := uint32(layersToCheck.Get(tctx.Parameters))
	first := nextFirstLayer(currentLayer(tctx, t, cl.Client(0)), layersPerEpoch)
	last := first + layersCount - 1
	logger.Debugw("watching layers between", "first", first, "last", last)

	createdch := make(chan *pb.Proposal, cl.Total()*(int(layersCount)))

	eg, ctx := errgroup.WithContext(tctx)
	for i := 0; i < cl.Total(); i++ {
		clientId := i
		client := cl.Client(clientId)
		logger.Debugw("watching", "client", client.Name, "clientId", clientId)
		watchProposals(ctx, eg, client, func(proposal *pb.Proposal) (bool, error) {
			if proposal.Layer.Number < first {
				return true, nil
			}
			if proposal.Layer.Number > last {
				logger.Debugw("proposal watcher is done", "client", client.Name)
				return false, nil
			}

			if proposal.Status == pb.Proposal_Created {
				logger.Debugw("received proposal event",
					"client", client.Name,
					"id", hex.EncodeToString(proposal.Smesher.Id),
					"layer", proposal.Layer.Number,
					"eligibilities", len(proposal.Eligibilities),
				)
				createdch <- proposal
			}
			return true, nil
		})
	}

	require.NoError(t, eg.Wait())
	close(createdch)

	type EpochSet = map[uint64]struct{}
	smeshers := map[string]EpochSet{}
	for proposal := range createdch {
		if smesher, ok := smeshers[string(proposal.Smesher.Id)]; !ok {
			smeshers[string(proposal.Smesher.Id)] = EpochSet{proposal.Epoch.Value: struct{}{}}
		} else {
			smesher[proposal.Epoch.Value] = struct{}{}
		}
	}

	firstEpochWithEligibility := uint32(math.Max(2.0, float64(first/layersPerEpoch)))
	epochsInTest := last/layersPerEpoch - firstEpochWithEligibility + 1
	for id, eligibleEpochs := range smeshers {
		assert.EqualValues(t, epochsInTest, len(eligibleEpochs), fmt.Sprintf("smesher ID: %v, its epochs: %v", hex.EncodeToString([]byte(id)), eligibleEpochs))
	}
}
