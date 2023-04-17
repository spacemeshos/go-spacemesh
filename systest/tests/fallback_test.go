package tests

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sort"
	"testing"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/systest/cluster"
	"github.com/spacemeshos/go-spacemesh/systest/testcontext"
)

func TestFallback(t *testing.T) {
	t.Parallel()

	tctx := testcontext.New(t, testcontext.Labels("sanity"))
	cl, err := cluster.Reuse(tctx,
		cluster.WithKeys(10),
		cluster.WithBootstrapperFlag(cluster.GenerateFallback()),
	)
	require.NoError(t, err)

	first := currentLayer(tctx, t, cl.Client(0))
	layersPerEpoch := uint32(testcontext.LayersPerEpoch.Get(tctx.Parameters))
	first = nextFirstLayer(first, layersPerEpoch)
	limit := 5 * layersPerEpoch
	last := first + limit
	tctx.Log.Debugw("watching layer between", "first", first, "last", last)

	createdch := make(chan *pb.Proposal, cl.Total()*int(limit+1))
	eg, ctx := errgroup.WithContext(tctx)
	for i := 0; i < cl.Total(); i++ {
		i := i
		client := cl.Client(i)
		tctx.Log.Debugw("watching", "client", client.Name, "i", i)
		watchProposals(ctx, eg, cl.Client(i), func(proposal *pb.Proposal) (bool, error) {
			if proposal.Layer.Number < first {
				return true, nil
			}
			if proposal.Layer.Number > last {
				return false, nil
			}
			if proposal.Status == pb.Proposal_Created {
				createdch <- proposal
			}
			return true, nil
		})
	}

	require.NoError(t, eg.Wait())
	close(createdch)

	created := map[uint32][]*pb.Proposal{}
	beacons := map[uint32]map[string]struct{}{}
	beaconSet := map[string]struct{}{}
	for proposal := range createdch {
		created[proposal.Layer.Number] = append(created[proposal.Layer.Number], proposal)
		if edata := proposal.GetData(); edata != nil {
			if _, exist := beacons[proposal.Epoch.Number]; !exist {
				beacons[proposal.Epoch.Number] = map[string]struct{}{}
			}
			beacons[proposal.Epoch.Number][prettyHex(edata.Beacon)] = struct{}{}
			beaconSet[prettyHex(edata.Beacon)] = struct{}{}
		}
	}
	for epoch, vals := range beacons {
		require.Len(t, vals, 1, "epoch=%d", epoch)
		for beacon := range vals {
			require.Equal(t, prettyHex(expectedBeacon(epoch)), beacon)
		}
	}
	// each epoch should have a unique beacon
	require.Lenf(t, beaconSet, len(beacons), "beacons=%v", beaconSet)

	lastEpoch := last / layersPerEpoch
	for epoch := uint32(2); epoch <= lastEpoch; epoch++ {
		refActives, err := queryEpochAtxs(tctx, cl.Client(0), epoch)
		cutoff := len(refActives) * 3 / 4 // bootstrapper only sets 3/4 of the epoch atx to be the fallback active set
		require.NoError(t, err, "query atxs from client", cl.Client(0).Name)
		tctx.Log.Debugw("got atx ids from client", "epoch", epoch, "client", cl.Client(0).Name, "size", len(refActives))
		for i := 0; i < cl.Total(); i++ {
			actives, err := queryActiveSet(tctx, cl.Client(i), epoch)
			require.NoError(t, err, "query actives from client", cl.Client(i).Name)
			tctx.Log.Debugw("got activeset ids from client", "epoch", epoch, "client", cl.Client(i).Name, "size", len(actives))
			require.ElementsMatchf(t, refActives[:cutoff], actives, "epoch=%v, client=%v", epoch, cl.Client(i).Name)
		}
	}
}

// bootstrapper always update fallback beacon with epoch number as the data.
func expectedBeacon(epoch uint32) []byte {
	b := make([]byte, types.BeaconSize)
	binary.LittleEndian.PutUint32(b, epoch)
	return b
}

func queryEpochAtxs(ctx *testcontext.Context, client *cluster.NodeClient, targetEpoch uint32) ([]types.ATXID, error) {
	msh := pb.NewMeshServiceClient(client)
	stream, err := msh.EpochStream(ctx, &pb.EpochStreamRequest{Epoch: targetEpoch - 1})
	if err != nil {
		return nil, fmt.Errorf("epoch stream %v: %w", client.Name, err)
	}
	atxids := make([]types.ATXID, 0, 10_000)
	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		atxids = append(atxids, types.ATXID(types.BytesToHash(resp.GetId().GetId())))
	}
	sort.Slice(atxids, func(i, j int) bool {
		return bytes.Compare(atxids[i].Bytes(), atxids[j].Bytes()) < 0
	})
	return atxids, nil
}

func queryActiveSet(ctx *testcontext.Context, client *cluster.NodeClient, epoch uint32) ([]types.ATXID, error) {
	dbg := pb.NewDebugServiceClient(client)
	resp, err := dbg.ActiveSet(ctx, &pb.ActiveSetRequest{Epoch: epoch})
	if err != nil {
		return nil, fmt.Errorf("active set grpc %v: %w", client.Name, err)
	}
	activeSet := make([]types.ATXID, 0, 10_000)
	for _, atxid := range resp.GetIds() {
		activeSet = append(activeSet, types.ATXID(types.BytesToHash(atxid.GetId())))
	}
	sort.Slice(activeSet, func(i, j int) bool {
		return bytes.Compare(activeSet[i].Bytes(), activeSet[j].Bytes()) < 0
	})
	return activeSet, nil
}
