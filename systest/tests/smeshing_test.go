package tests

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/genvm/sdk"
	sdkmultisig "github.com/spacemeshos/go-spacemesh/genvm/sdk/multisig"
	sdkvesting "github.com/spacemeshos/go-spacemesh/genvm/sdk/vesting"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/multisig"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/vault"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/vesting"
	"github.com/spacemeshos/go-spacemesh/systest/cluster"
	"github.com/spacemeshos/go-spacemesh/systest/testcontext"
)

func TestSmeshing(t *testing.T) {
	// TODO(mafa): add new test with multi-smeshing nodes
	t.Parallel()

	tctx := testcontext.New(t, testcontext.Labels("sanity"))
	tctx.RemoteSize = tctx.ClusterSize / 4 // 25% of nodes are remote
	vests := vestingAccs{
		prepareVesting(t, 3, 8, 20, 1e15, 10e15),
		prepareVesting(t, 5, 8, 20, 1e15, 10e15),
		prepareVesting(t, 1, 8, 20, 1e15, 1e15),
		prepareVesting(t, 1, 8, 20, 0, 1e15),
	}
	cl, err := cluster.ReuseWait(tctx,
		cluster.WithKeys(tctx.ClusterSize),
		cluster.WithGenesisBalances(vests.genesisBalances()...),
	)
	require.NoError(t, err)
	testSmeshing(t, tctx, cl)
	testTransactions(t, tctx, cl, 8)
	testVesting(t, tctx, cl, vests...)
}

func testSmeshing(t *testing.T, tctx *testcontext.Context, cl *cluster.Cluster) {
	const limit = 15

	first := currentLayer(tctx, t, cl.Client(0))
	layersPerEpoch := uint32(testcontext.LayersPerEpoch.Get(tctx.Parameters))
	first = nextFirstLayer(first, layersPerEpoch)
	last := first + limit
	tctx.Log.Debugw("watching layer between", "first", first, "last", last)

	createdCh := make(chan *pb.Proposal, cl.Total()*(limit+1))
	includedAll := make([]map[uint32][]*pb.Proposal, cl.Total())
	for i := range cl.Total() {
		includedAll[i] = map[uint32][]*pb.Proposal{}
	}

	eg, ctx := errgroup.WithContext(tctx)
	for i := range cl.Total() {
		client := cl.Client(i)
		tctx.Log.Debugw("watching", "client", client.Name, "i", i)
		watchProposals(ctx, eg, client, tctx.Log.Desugar(), func(proposal *pb.Proposal) (bool, error) {
			if proposal.Layer.Number < first {
				return true, nil
			}
			tctx.Log.Debugw("received proposal event",
				"client", client.Name,
				"layer", proposal.Layer.Number,
				"smesher", prettyHex(proposal.Smesher.Id),
				"eligibilities", len(proposal.Eligibilities),
				"status", pb.Proposal_Status_name[int32(proposal.Status)],
			)
			if proposal.Layer.Number > last {
				return false, nil
			}
			if proposal.Status == pb.Proposal_Created {
				createdCh <- proposal
			} else {
				includedAll[i][proposal.Layer.Number] = append(includedAll[i][proposal.Layer.Number], proposal)
			}
			return true, nil
		})
	}

	require.NoError(t, eg.Wait())
	close(createdCh)

	created := map[uint32][]*pb.Proposal{}
	beacons := map[uint32]map[string]struct{}{}
	beaconSet := map[string]struct{}{}
	for proposal := range createdCh {
		created[proposal.Layer.Number] = append(created[proposal.Layer.Number], proposal)
		if edata := proposal.GetData(); edata != nil {
			if _, exist := beacons[proposal.Epoch.Number]; !exist {
				beacons[proposal.Epoch.Number] = map[string]struct{}{}
			}
			beacons[proposal.Epoch.Number][prettyHex(edata.Beacon)] = struct{}{}
			beaconSet[prettyHex(edata.Beacon)] = struct{}{}
		}
	}
	requireEqualEligibilities(tctx, t, created)
	requireEqualProposals(t, created, includedAll)
	for epoch := range beacons {
		require.Len(t, beacons[epoch], 1, "epoch=%d", epoch)
	}
	// each epoch should have a unique beacon
	require.Len(t, beaconSet, len(beacons), "beacons=%v", beaconSet)
}

func requireEqualProposals(tb testing.TB, reference map[uint32][]*pb.Proposal, received []map[uint32][]*pb.Proposal) {
	tb.Helper()
	for layer := range reference {
		sort.Slice(reference[layer], func(i, j int) bool {
			return bytes.Compare(reference[layer][i].Smesher.Id, reference[layer][j].Smesher.Id) == -1
		})
	}
	for i, included := range received {
		for layer := range included {
			sort.Slice(included[layer], func(i, j int) bool {
				return bytes.Compare(included[layer][i].Smesher.Id, included[layer][j].Smesher.Id) == -1
			})
		}
		for layer, proposals := range reference {
			require.Lenf(tb, included[layer], len(proposals), "client=%d layer=%d", i, layer)
			for j := range proposals {
				assert.Equalf(tb, proposals[j].Id, included[layer][j].Id, "client=%d layer=%d", i, layer)
			}
		}
	}
}

func requireEqualEligibilities(tctx *testcontext.Context, tb testing.TB, proposals map[uint32][]*pb.Proposal) {
	tb.Helper()

	aggregated := map[string]int{}
	for _, perlayer := range proposals {
		for _, proposal := range perlayer {
			aggregated[string(proposal.Smesher.Id)] += len(proposal.Eligibilities)
		}
	}

	tctx.Log.Desugar().
		Info("aggregated eligibilities", zap.Object("per-smesher",
			zapcore.ObjectMarshalerFunc(func(enc zapcore.ObjectEncoder) error {
				for smesher, eligibilities := range aggregated {
					enc.AddInt(prettyHex([]byte(smesher)), eligibilities)
				}
				return nil
			}),
		))

	referenceEligibilities := -1
	for smesher, eligibilities := range aggregated {
		if referenceEligibilities < 0 {
			referenceEligibilities = eligibilities
		} else {
			assert.Equal(tb, referenceEligibilities, eligibilities, prettyHex([]byte(smesher)))
		}
	}
}

func testVesting(tb testing.TB, tctx *testcontext.Context, cl *cluster.Cluster, accs ...vestingAcc) {
	tb.Helper()
	var (
		eg      errgroup.Group
		genesis = cl.GenesisID()
	)
	for i, acc := range accs {
		client := cl.Client(i % cl.Total())
		eg.Go(func() error {
			var subeg errgroup.Group
			watchLayers(tctx, &subeg, client, tctx.Log.Desugar(), func(layer *pb.LayerStreamResponse) (bool, error) {
				return layer.Layer.Number.Number < uint32(acc.start), nil
			})
			if err := subeg.Wait(); err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(tctx, 10*time.Minute)
			defer cancel()
			var nonce uint64
			id, err := submitTransaction(ctx, acc.selfSpawn(genesis, nonce), client)
			if err != nil {
				return fmt.Errorf("selfspawn multisig: %w", err)
			}
			nonce++

			initial, err := currentBalance(ctx, client, acc.address)
			if err != nil {
				return err
			}
			tctx.Log.Debugw("submitted selfspawn",
				"address", acc.address,
				"initial", initial,
			)
			waitTransaction(ctx, &subeg, client, id)
			if err := subeg.Wait(); err != nil {
				return err
			}
			id, err = submitTransaction(ctx, acc.spawnVault(genesis, nonce), client)
			if err != nil {
				return fmt.Errorf("spawn vault: %w", err)
			}
			nonce++
			tctx.Log.Debugw("submitted spawn vault",
				"address", acc.vault,
			)
			waitTransaction(ctx, &subeg, client, id)
			if err := subeg.Wait(); err != nil {
				return err
			}
			leftover := acc.total
			for leftover > 0 {
				step := acc.total / (acc.end - acc.start)
				if leftover > step {
					leftover -= step
				} else {
					step = leftover
					leftover = 0
				}
				tctx.Log.Debugw("submitted drain vault",
					"amount", step,
					"leftover", leftover,
				)
				id, err := submitTransaction(ctx, acc.drain(genesis, uint64(step), nonce), client)
				if err != nil {
					return fmt.Errorf("drain: %w", err)
				}
				nonce++
				waitTransaction(ctx, &subeg, client, id)
				if err := subeg.Wait(); err != nil {
					return err
				}
			}
			remaining, err := currentBalance(ctx, client, acc.vault)
			if err != nil {
				return err
			}
			current, err := currentBalance(ctx, client, acc.address)
			if err != nil {
				return err
			}
			tctx.Log.Infow("results for account after tests",
				"vest", acc.address,
				"vault", acc.vault,
				"initial", initial,
				"current", current,
				"vested", acc.total,
				"remaining", remaining,
				"client", client.Name,
			)
			if remaining != 0 {
				return fmt.Errorf("vault at %v must be empty, instead has %d", acc.vault, remaining)
			}
			if delta := int(current - initial); delta+1e7 < acc.total {
				return fmt.Errorf(
					"account at %v should drain all values from vault (compensated for tx gas), instead has %d",
					acc.address,
					delta,
				)
			}
			return nil
		})
	}
	require.NoError(tb, eg.Wait())
}

type vestingAccs []vestingAcc

func (v vestingAccs) genesisBalances() (rst []cluster.GenAccount) {
	for _, acc := range v {
		rst = append(rst,
			cluster.GenAccount{Address: acc.address, Balance: 1e8},
			cluster.GenAccount{Address: acc.vault, Balance: uint64(acc.total)},
		)
	}
	return rst
}

type vestingAcc struct {
	required       int
	pks            []ed25519.PrivateKey
	pubs           []ed25519.PublicKey
	address, vault types.Address
	start, end     int
	initial, total int
}

func (v vestingAcc) selfSpawn(genesis types.Hash20, nonce uint64) []byte {
	var agg *sdkmultisig.Aggregator
	for i := 0; i < v.required; i++ {
		pk := v.pks[i]
		part := sdkmultisig.SelfSpawn(
			uint8(i),
			pk,
			vesting.TemplateAddress,
			uint8(v.required),
			v.pubs,
			nonce,
			sdk.WithGenesisID(genesis),
		)
		if agg == nil {
			agg = part
		} else {
			agg.Add(*part.Part(uint8(i)))
		}
	}
	return agg.Raw()
}

func (v vestingAcc) spawnVault(genesis types.Hash20, nonce uint64) []byte {
	args := vault.SpawnArguments{
		Owner:               v.address,
		InitialUnlockAmount: uint64(v.initial),
		TotalAmount:         uint64(v.total),
		VestingStart:        types.LayerID(v.start),
		VestingEnd:          types.LayerID(v.end),
	}
	var agg *sdkmultisig.Aggregator
	for i := 0; i < v.required; i++ {
		pk := v.pks[i]
		part := sdkmultisig.Spawn(
			uint8(i),
			pk,
			v.address,
			vault.TemplateAddress,
			&args,
			nonce,
			sdk.WithGenesisID(genesis),
		)
		if agg == nil {
			agg = part
		} else {
			agg.Add(*part.Part(uint8(i)))
		}
	}
	return agg.Raw()
}

func (v vestingAcc) drain(genesis types.Hash20, amount, nonce uint64) []byte {
	var agg *sdkvesting.Aggregator
	for i := 0; i < v.required; i++ {
		pk := v.pks[i]
		part := sdkvesting.DrainVault(
			uint8(i),
			pk,
			v.address,
			v.vault,
			v.address,
			amount,
			nonce,
			sdk.WithGenesisID(genesis),
		)
		if agg == nil {
			agg = part
		} else {
			agg.Add(*part.Part(uint8(i)))
		}
	}
	return agg.Raw()
}

func genKeys(tb testing.TB, n int) (pks []ed25519.PrivateKey, pubs []ed25519.PublicKey) {
	tb.Helper()
	for i := 0; i < n; i++ {
		pub, pk, err := ed25519.GenerateKey(nil)
		require.NoError(tb, err)
		pks = append(pks, pk)
		pubs = append(pubs, pub)
	}
	return pks, pubs
}

func prepareVesting(tb testing.TB, keys, start, end, initial, total int) vestingAcc {
	tb.Helper()
	pks, pubs := genKeys(tb, keys)
	var hashes []types.Hash32
	for _, pub := range pubs {
		var hs types.Hash32
		copy(hs[:], pub)
		hashes = append(hashes, hs)
	}
	vestingArgs := &multisig.SpawnArguments{
		Required:   uint8(keys/2 + 1),
		PublicKeys: hashes,
	}
	vestingAddress := core.ComputePrincipal(vesting.TemplateAddress, vestingArgs)
	vaultArgs := &vault.SpawnArguments{
		Owner:               vestingAddress,
		InitialUnlockAmount: uint64(initial),
		TotalAmount:         uint64(total),
		VestingStart:        types.LayerID(start),
		VestingEnd:          types.LayerID(end),
	}
	return vestingAcc{
		required: int(vestingArgs.Required),
		pks:      pks,
		pubs:     pubs,
		address:  vestingAddress,
		vault:    core.ComputePrincipal(vault.TemplateAddress, vaultArgs),
		start:    start,
		end:      end,
		initial:  initial,
		total:    total,
	}
}
