package tests

import (
	"crypto/ed25519"
	"encoding/base64"
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
	tctx := testcontext.New(t)
	tctx.Log.Debug("TestPoetsFailures start")

	cl, err := cluster.ReuseWait(tctx, cluster.WithKeys(tctx.ClusterSize))
	require.NoError(t, err)
	tctx.Log.Debug("Obtained cluster")

	testPoetDies(t, tctx, cl)
	tctx.Log.Debug("TestPoetsFailures is done")
}

func testPoetDies(t *testing.T, tctx *testcontext.Context, cl *cluster.Cluster) {
	layersPerEpoch := uint32(testcontext.LayersPerEpoch.Get(tctx.Parameters))
	layersCount := uint32(layersToCheck.Get(tctx.Parameters))
	first := nextFirstLayer(currentLayer(tctx, t, cl.Client(0)), layersPerEpoch)
	last := first + layersCount - 1
	tctx.Log.Debugw("watching layers between", "first", first, "last", last)

	createdch := make(chan *pb.Proposal, cl.Total()*(int(layersCount)))

	eg, ctx := errgroup.WithContext(tctx)
	for i := 0; i < cl.Total(); i++ {
		client := cl.Client(i)
		tctx.Log.Debugw("watching", "client", client.Name, "clientId", i)
		watchProposals(ctx, eg, client, tctx.Log.Desugar(), func(proposal *pb.Proposal) (bool, error) {
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

	watchLayers(ctx, eg, cl.Client(0), tctx.Log.Desugar(), func(layer *pb.LayerStreamResponse) (bool, error) {
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
	beacons := map[uint32]map[string]struct{}{}
	for proposal := range createdch {
		created[proposal.Layer.Number] = append(created[proposal.Layer.Number], proposal)
		if edata := proposal.GetData(); edata != nil {
			if _, exist := beacons[proposal.Epoch.Number]; !exist {
				beacons[proposal.Epoch.Number] = map[string]struct{}{}
			}
			tctx.Log.Desugar().Debug("new beacon",
				zap.String("smesher", prettyHex(proposal.Smesher.Id)),
				zap.String("beacon", prettyHex(edata.Beacon)),
				zap.Uint32("epoch", proposal.Epoch.Number),
			)
			beacons[proposal.Epoch.Number][prettyHex(edata.Beacon)] = struct{}{}
		}
	}

	requireEqualEligibilities(tctx, t, created)

	for epoch := range beacons {
		assert.Len(t, beacons[epoch], 1, "epoch=%d", epoch)
	}
}

func TestNodesUsingDifferentPoets(t *testing.T) {
	t.Parallel()
	tctx := testcontext.New(t)
	if tctx.PoetSize < 2 {
		t.Skip("Skipping test for using different poets - test configured with less then 2 poets")
	}

	cl := cluster.New(tctx, cluster.WithKeys(tctx.ClusterSize))
	require.NoError(t, cl.AddBootnodes(tctx, 2))
	require.NoError(t, cl.AddBootstrappers(tctx))
	require.NoError(t, cl.AddPoets(tctx))

	for i := 0; i < tctx.ClusterSize-2; i++ {
		poetId := i % tctx.PoetSize
		poet := cluster.MakePoetEndpoint(poetId)
		tctx.Log.Debugw("adding smesher node", "id", i, "poet", poet)
		err := cl.AddSmeshers(
			tctx,
			1,
			cluster.NoDefaultPoets(),
			cluster.WithFlags(cluster.PoetEndpoints(poetId)),
		)
		require.NoError(t, err)
	}
	require.NoError(t, cl.WaitAll(tctx))

	layersCount := uint32(layersToCheck.Get(tctx.Parameters))
	layersPerEpoch := uint32(testcontext.LayersPerEpoch.Get(tctx.Parameters))
	first := nextFirstLayer(currentLayer(tctx, t, cl.Client(0)), layersPerEpoch)
	last := first + layersCount - 1
	tctx.Log.Debugw("watching layers between", "first", first, "last", last)

	createdch := make(chan *pb.Proposal, cl.Total()*(int(layersCount)))

	eg, ctx := errgroup.WithContext(tctx)
	for i := 0; i < cl.Total(); i++ {
		client := cl.Client(i)
		tctx.Log.Debugw("watching", "client", client.Name)
		watchProposals(ctx, eg, client, tctx.Log.Desugar(), func(proposal *pb.Proposal) (bool, error) {
			if proposal.Layer.Number < first {
				return true, nil
			}
			if proposal.Layer.Number > last {
				tctx.Log.Debugw("proposal watcher is done", "client", client.Name)
				return false, nil
			}

			if proposal.Status == pb.Proposal_Created {
				tctx.Log.Debugw("received proposal event",
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

	type EpochSet = map[uint32]struct{}
	smeshers := map[string]EpochSet{}
	for proposal := range createdch {
		if smesher, ok := smeshers[string(proposal.Smesher.Id)]; !ok {
			smeshers[string(proposal.Smesher.Id)] = EpochSet{proposal.Epoch.Number: struct{}{}}
		} else {
			smesher[proposal.Epoch.Number] = struct{}{}
		}
	}

	firstEpochWithEligibility := uint32(math.Max(2.0, float64(first/layersPerEpoch)))
	epochsInTest := last/layersPerEpoch - firstEpochWithEligibility + 1
	for id, eligibleEpochs := range smeshers {
		assert.Len(
			t,
			eligibleEpochs,
			int(epochsInTest),
			fmt.Sprintf("smesher ID: %v, its epochs: %v", hex.EncodeToString([]byte(id)), eligibleEpochs),
		)
	}
}

// Test verifying that nodes can register in both poets
// - supporting PoW only
// - supporting certificates
// TODO: When PoW support is removed, convert this test to verify only the cert path.
// https://github.com/spacemeshos/go-spacemesh/issues/5212
func TestRegisteringInPoetWithPowAndCert(t *testing.T) {
	t.Parallel()
	tctx := testcontext.New(t)
	tctx.PoetSize = 2

	cl := cluster.New(tctx, cluster.WithKeys(10))
	require.NoError(t, cl.AddBootnodes(tctx, 2))
	require.NoError(t, cl.AddBootstrappers(tctx))

	pubkey, privkey, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	require.NoError(t, cl.AddCertifier(tctx, base64.StdEncoding.EncodeToString(privkey.Seed())))
	// First poet supports PoW only (legacy)
	require.NoError(t, cl.AddPoet(tctx))
	// Second poet supports certs
	require.NoError(
		t,
		cl.AddPoet(
			tctx,
			cluster.PoetCertifierURL("http://certifier-0"),
			cluster.PoetCertifierPubkey(base64.StdEncoding.EncodeToString(pubkey)),
		),
	)
	require.NoError(t, cl.AddSmeshers(tctx, tctx.ClusterSize-2))
	require.NoError(t, cl.WaitAll(tctx))

	epoch := 2
	layersPerEpoch := testcontext.LayersPerEpoch.Get(tctx.Parameters)
	last := uint32(layersPerEpoch * epoch)
	tctx.Log.Debugw("waiting for epoch", "epoch", epoch, "layer", last)

	eg, ctx := errgroup.WithContext(tctx)
	for i := 0; i < cl.Total(); i++ {
		client := cl.Client(i)
		tctx.Log.Debugw("watching", "client", client.Name)
		watchProposals(ctx, eg, client, tctx.Log.Desugar(), func(proposal *pb.Proposal) (bool, error) {
			return proposal.Layer.Number < last, nil
		})
	}

	require.NoError(t, eg.Wait())

	// Check that smeshers are registered in both poets
	valid := map[string]string{"result": "valid"}
	invalid := map[string]string{"result": "invalid"}

	metricsEndpoint := cluster.MakePoetMetricsEndpoint(tctx.Namespace, 0)
	powRegs, err := fetchCounterMetric(tctx, metricsEndpoint, "poet_registration_with_pow_total", valid)
	require.NoError(t, err)
	require.GreaterOrEqual(t, powRegs, float64(cl.Smeshers()*epoch))

	powRegsInvalid, err := fetchCounterMetric(tctx, metricsEndpoint, "poet_registration_with_pow_total", invalid)
	require.ErrorIs(t, err, errMetricNotFound, "metric for invalid PoW registrations value: %v", powRegsInvalid)

	metricsEndpoint = cluster.MakePoetMetricsEndpoint(tctx.Namespace, 1)
	certRegs, err := fetchCounterMetric(tctx, metricsEndpoint, "poet_registration_with_cert_total", valid)
	require.NoError(t, err)
	require.GreaterOrEqual(t, certRegs, float64(cl.Smeshers()*epoch))

	certRegsInvalid, err := fetchCounterMetric(tctx, metricsEndpoint, "poet_registration_with_cert_total", invalid)
	require.ErrorIs(t, err, errMetricNotFound, "metric for invalid cert registrations value: %v", certRegsInvalid)
}
