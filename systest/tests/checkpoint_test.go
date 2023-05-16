package tests

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang/protobuf/ptypes/empty"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/systest/cluster"
	"github.com/spacemeshos/go-spacemesh/systest/testcontext"
)

func TestCheckpoint(t *testing.T) {
	t.Parallel()

	tctx := testcontext.New(t, testcontext.Labels("sanity"))
	addedLater := 2
	size := min(tctx.ClusterSize, 30)
	oldSize := size - addedLater
	if tctx.ClusterSize > oldSize {
		tctx.Log.Info("cluster size changed to ", oldSize)
		tctx.ClusterSize = oldSize
	}

	// at the last layer of epoch 3, in the beginning of poet round 2.
	// it is important to avoid check-pointing in the middle of cycle gap
	// otherwise nodes' proof generation will be interrupted and miss
	// the start of the next poet round
	snapshotLayer := uint32(15)
	restoreLayer := uint32(18)

	// need to bootstrap the checkpoint epoch and the next epoch as the beacon protocol was interrupted in the last epoch
	cl, err := cluster.ReuseWait(tctx, cluster.WithKeys(10), cluster.WithBootstrapEpochs([]int{2, 4, 5}))
	require.NoError(t, err)

	layersPerEpoch := uint32(testcontext.LayersPerEpoch.Get(tctx.Parameters))
	require.EqualValues(t, 4, layersPerEpoch, "checkpoint layer require tuning as layersPerEpoch is changed")
	layerDuration := testcontext.LayerDuration.Get(tctx.Parameters)

	eg, ctx := errgroup.WithContext(tctx)
	first := layersPerEpoch * 2
	stop := first + 2
	receiver := types.GenerateAddress([]byte{11, 1, 1})
	tctx.Log.Infow("sending transactions", "from", first, "to", stop-1)
	sendTransactions(ctx, eg, tctx.Log, cl, first, stop, receiver, 1, 100)
	require.NoError(t, eg.Wait())

	require.NoError(t, waitLayer(tctx, cl.Client(0), snapshotLayer))

	tctx.Log.Debugw("getting account balances")
	before, err := getBalance(tctx, cl)
	require.NoError(t, err)
	for addr, state := range before {
		tctx.Log.Infow("account received",
			"address", addr.String(),
			"nonce", state.nonce,
			"balance", state.balance,
		)
	}

	tctx.Log.Infow("checkpoint cluster", "snapshot", snapshotLayer, "restart", restoreLayer)
	// query checkpoint files. they all should be the same
	var checkpoints [][]byte
	for i := 0; i < cl.Total(); i++ {
		client := cl.Client(i)
		data, err := checkpointAndRecover(tctx, client, snapshotLayer, restoreLayer)
		require.NoError(t, err)
		checkpoints = append(checkpoints, data)
	}

	var diffs []string
	for i := 1; i < len(checkpoints); i++ {
		if !bytes.Equal(checkpoints[0], checkpoints[i]) {
			diffs = append(diffs, cl.Client(i).Name)
			tctx.Log.Errorw("diff checkpoint data",
				fmt.Sprintf("reference %v", cl.Client(0).Name), string(checkpoints[0]),
				fmt.Sprintf("client %v", cl.Client(i).Name), string(checkpoints[i]))
		}
	}
	require.Empty(t, diffs)

	tctx.Log.Infow("wait for cluster to recover", "wait", layerDuration)
	select {
	case <-time.After(layerDuration):
	case <-tctx.Done():
		t.Fail()
	}

	tctx.Log.Infow("rediscovering cluster")
	cl, err = cluster.ReuseWait(tctx, cluster.WithKeys(10))
	require.NoError(t, err)

	tctx.Log.Infow("checking account balances")
	// check if the account balance is correct
	after, err := getBalance(tctx, cl)
	require.NoError(t, err)
	for addr, state := range before {
		st, ok := after[addr]
		if !ok {
			assert.Failf(t, "account missing after restore", addr.String())
		} else if st != state {
			assert.Failf(t, "account incorrect after restore",
				"addr %v before %+v , after %+v", addr.String(), state, st)
		}
	}

	require.NoError(t, waitLayer(tctx, cl.Client(0), 5*layersPerEpoch))
	testSmeshing(t, tctx, cl, 7)

	ip, err := cl.Bootstrapper(0).Resolve(tctx)
	require.NoError(t, err)
	tctx.Log.Debugw("resolved bootstrapper", "ip", ip)

	endpoint := fmt.Sprintf("http://%s:%d", ip, 80)
	updateUrl := fmt.Sprintf("%s/updateCheckpoint", endpoint)
	tctx.Log.Infow("submit checkpoint data", "update url", updateUrl)
	require.NoError(t, updateCheckpointServer(tctx, updateUrl, checkpoints[0]))

	queryUrl := fmt.Sprintf("%s/checkpoint", endpoint)
	tctx.Log.Debugw("query checkpoint data", "query url", queryUrl)
	data, err := query(tctx, queryUrl)
	require.NoError(t, err)
	require.True(t, bytes.Equal(checkpoints[0], data))

	// increase the cluster size to the original test size
	tctx.Log.Info("cluster size changed to ", size)
	tctx.ClusterSize = size

	tctx.Log.Infow("adding smesher with checkpoint url", "query url", queryUrl)
	require.NoError(t, cl.AddSmeshers(tctx, addedLater, cluster.DeploymentFlag{Name: "--checkpoint", Value: queryUrl}))

	require.NoError(t, waitLayer(tctx, cl.Client(0), layersPerEpoch*9))
	eg, _ = errgroup.WithContext(tctx)
	created := map[uint32][]*pb.Proposal{}
	for i := cl.Total() - addedLater; i < cl.Total(); i++ {
		i := i
		client := cl.Client(i)
		watchProposals(tctx, eg, client, func(proposal *pb.Proposal) (bool, error) {
			if proposal.Epoch.Number > 9 {
				return false, nil
			}
			if proposal.Status == pb.Proposal_Created {
				tctx.Log.Debugw("received proposal event",
					"client", client.Name,
					"layer", proposal.Layer.Number,
					"epoch", proposal.Epoch.Number,
					"smesher", prettyHex(proposal.Smesher.Id),
					"eligibilities", len(proposal.Eligibilities),
					"status", pb.Proposal_Status_name[int32(proposal.Status)],
				)
				created[proposal.Layer.Number] = append(created[proposal.Layer.Number], proposal)
			}
			return true, nil
		})
	}
	require.NoError(t, eg.Wait())
	requireEqualEligibilities(tctx, t, created)
}

func query(ctx context.Context, endpoint string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func updateCheckpointServer(ctx *testcontext.Context, endpoint string, chdata []byte) error {
	formData := url.Values{"checkpoint": []string{string(chdata)}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(formData.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

func checkpointAndRecover(ctx *testcontext.Context, client *cluster.NodeClient, snapshotLayer, restoreLayer uint32) ([]byte, error) {
	smshr := pb.NewAdminServiceClient(client)
	stream, err := smshr.CheckpointStream(ctx, &pb.CheckpointStreamRequest{SnapshotLayer: snapshotLayer, RestoreLayer: restoreLayer})
	if err != nil {
		return nil, fmt.Errorf("stream checkpoiont %v: %w", client.Name, err)
	}
	var result bytes.Buffer
	total := 0
	for {
		msg, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("receive stream %v, %w", client.Name, err)
		}
		if n, err := result.Write(msg.Data); err != nil {
			return nil, fmt.Errorf("write data to buffer: %w", err)
		} else {
			total += n
		}
	}
	// recover
	_, err = smshr.Recover(ctx, &pb.RecoverRequest{
		Uri: filepath.Join("file:///data/state/checkpoint", fmt.Sprintf("snapshot-%d-restore-%d", snapshotLayer, restoreLayer)),
	})
	if err != nil {
		ctx.Log.Infow("recover returned error", "err", err.Error())
	}
	ctx.Log.Infow("checkpoint file received", "client", client.Name, "size", len(result.Bytes()), "n", total)
	return result.Bytes(), nil
}

type acctState struct {
	nonce   uint64
	balance uint64
}

func getBalance(tctx *testcontext.Context, cl *cluster.Cluster) (map[types.Address]acctState, error) {
	dbg := pb.NewDebugServiceClient(cl.Client(0))
	response, err := dbg.Accounts(tctx, &empty.Empty{})
	if err != nil {
		return nil, err
	}
	expectedBalances := map[types.Address]acctState{}
	for _, acct := range response.GetAccountWrapper() {
		addr, err := types.StringToAddress(acct.AccountId.GetAddress())
		if err != nil {
			return nil, err
		}
		expectedBalances[addr] = acctState{
			nonce:   acct.GetStateCurrent().GetCounter(),
			balance: acct.GetStateCurrent().GetBalance().GetValue(),
		}
	}
	return expectedBalances, nil
}
