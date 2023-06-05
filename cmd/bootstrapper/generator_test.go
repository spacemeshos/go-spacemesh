package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/bootstrap"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(epochLayers)
	os.Exit(m.Run())
}

const (
	epochLayers      = 3
	grpcPort         = 9992
	jsonport         = 9993
	target           = "localhost"
	activeSetSize    = 11
	bitcoinResponse1 = `
{
  "name": "BTC.main",
  "height": 782692,
  "hash": "0000000000000000000039136b9d5233215cb4b016aa9df2fa925540ade10269",
  "time": "2023-03-27T04:08:32.208090614Z",
  "latest_url": "https://api.blockcypher.com/v1/btc/main/blocks/0000000000000000000039136b9d5233215cb4b016aa9df2fa925540ade10269",
  "previous_hash": "00000000000000000004325430e1322d199932438369502443bd74018b9aa8fb",
  "previous_url": "https://api.blockcypher.com/v1/btc/main/blocks/00000000000000000004325430e1322d199932438369502443bd74018b9aa8fb",
  "peer_count": 244,
  "unconfirmed_count": 4920,
  "high_fee_per_kb": 24220,
  "medium_fee_per_kb": 15639,
  "low_fee_per_kb": 12038,
  "last_fork_height": 781487,
  "last_fork_hash": "00000000000000000000cb36faae26a886cd6c26bdde276f656258e9b4644eec"
}
`
	bitcoinResponse2 = `
{
  "hash": "00000000000000000001051a8ec5598b6ca39a35c487fd7ba70fb1008f8a2576",
  "height": 782685,
  "chain": "BTC.main",
  "total": 43743158577,
  "fees": 6247942,
  "size": 3687282,
  "vsize": 998545,
  "ver": 536993792,
  "time": "2023-03-27T03:03:14Z",
  "received_time": "2023-03-27T03:03:59.536Z",
  "relayed_by": "18.208.171.42:8333",
  "bits": 386269758,
  "nonce": 3474318277,
  "n_tx": 484,
  "prev_block": "00000000000000000003a937de36bd5afde1fc706f4bbef2b6c5d4b20123b8db",
  "mrkl_root": "75c47e0aafe4348ceb29df5f7206a447b954bf541f8b8a127658caadc21586eb",
  "txids": [
    "cb8688929cfdd3a7eee3c9c02600926ce31b53605431ded621b74b22d92262d0",
    "1b50520a957c2797ec4ac930490f09dd9e4d2abec7f56de69ad96915212ac1fe",
    "fab8cbe54e8ffbcccbe8bc60f5a1ac9096946249c0885b90fb0912f118f8bcc5",
    "0f7e4882bc259c91dd74ae2ef260eb98e77d0bcc68abd6499fb2c50ad2d48ade",
    "72655d53a7a4645b49112d07abe70ccb796fc151b2de1e45bc5e37d17a8799ac",
    "8be9b55c735ab632014751982c83d29b7f790a5470cd3d969c71515002032045",
    "18435579d4806ab1298f1cab912cb91b234c24489f86241d7b828a1d487674e1",
    "65d189e9c52686d83818e278773d491b163055d3ed055a4bbc20d24c9b47d742",
    "eb9cc8eb0f13b56dd1653606a32c663b686fe5579634915c4f7aa1bd5478e9b0",
    "526725043c5d837d5a93a4a33bb7a150b34df9890a60ff78b42d747de6b9e674",
    "4c5f3476f3875135b42c464a138621bd4d6d54621baab082dafa60d91ec8a45f",
    "28f204906a7f4c86bd3835fc926be867d59b1a3dcad356d7ffae00a8912055cd",
    "ed23250edac7cae2d14397e7a845c94e82b2de8b4027295e701819426f4fd5e3",
    "e9d1d38137a1351f7d9a6875da37ee96ab47a8f4dfe9044872ed86538668c113",
    "6f9f1d7ce72e81484dc51c2c72ac5003dd345cc531ba9b7961c905b19541e6eb",
    "a73754994b476e19ef77325c42927fc93715c7b13826cbcc92f23fa83c1febb2",
    "f1a2a555dd3f0f151ef26705c29d4e3234f457ed571f9ed1bca0009358dc2a87",
    "fb162257556821d78598a455f8308526ae264389011ee07c98b5b592f437eac2",
    "99a2901198eff8c04ca23bfc46f78ec69af0c4c888df5690e58985a9e2b1c479",
    "5bb6cb6fe95ed9948b5d58630f20a37442575204acdd2a66c6a855e5d91df380"
  ],
  "depth": 597,
  "prev_block_url": "https://api.blockcypher.com/v1/btc/main/blocks/00000000000000000003a937de36bd5afde1fc706f4bbef2b6c5d4b20123b8db",
  "tx_url": "https://api.blockcypher.com/v1/btc/main/txs/",
  "next_txids": "https://api.blockcypher.com/v1/btc/main/blocks/00000000000000000001051a8ec5598b6ca39a35c487fd7ba70fb1008f8a2576?txstart=20\u0026limit=20"
}`
	expectedBeacon = "8f8a2576" // from bitcoinResponse2
	emptyBeacon    = "00000000"
)

type MeshAPIMock struct{}

func (m *MeshAPIMock) LatestLayer() types.LayerID                        { panic("not implemented") }
func (m *MeshAPIMock) LatestLayerInState() types.LayerID                 { panic("not implemented") }
func (m *MeshAPIMock) ProcessedLayer() types.LayerID                     { panic("not implemented") }
func (m *MeshAPIMock) GetRewards(types.Address) ([]*types.Reward, error) { panic("not implemented") }
func (m *MeshAPIMock) GetLayer(types.LayerID) (*types.Layer, error)      { panic("not implemented") }
func (m *MeshAPIMock) GetATXs(context.Context, []types.ATXID) (map[types.ATXID]*types.VerifiedActivationTx, []types.ATXID) {
	panic("not implemented")
}
func (m *MeshAPIMock) MeshHash(types.LayerID) (types.Hash32, error) { panic("not implemented") }
func (m *MeshAPIMock) EpochAtxs(types.EpochID) ([]types.ATXID, error) {
	return types.RandomActiveSet(activeSetSize), nil
}

func launchServer(tb testing.TB) func() {
	grpcService := grpcserver.New(fmt.Sprintf("127.0.0.1:%d", grpcPort))
	jsonService := grpcserver.NewJSONHTTPServer(fmt.Sprintf("127.0.0.1:%d", jsonport))
	s := grpcserver.NewMeshService(&MeshAPIMock{}, nil, nil, 0, types.Hash20{}, 0, 0, 0)

	pb.RegisterMeshServiceServer(grpcService.GrpcServer, s)
	// start gRPC and json servers
	grpcStarted := grpcService.Start()
	jsonStarted := jsonService.StartService(context.Background(), s)

	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()

	// wait for server to be ready (critical on CI)
	for _, ch := range []<-chan struct{}{grpcStarted, jsonStarted} {
		select {
		case <-ch:
		case <-timer.C:
		}
	}

	return func() {
		require.NoError(tb, jsonService.Shutdown(context.Background()))
		_ = grpcService.Close()
	}
}

func verifyUpdate(t *testing.T, data []byte, epoch types.EpochID, expBeacon string, expAsSize int) {
	require.NoError(t, bootstrap.ValidateSchema(data))
	var update bootstrap.Update
	require.NoError(t, json.Unmarshal(data, &update))
	require.Equal(t, SchemaVersion, update.Version)
	require.EqualValues(t, epoch, update.Data.Epoch.ID)
	require.Equal(t, expBeacon, update.Data.Epoch.Beacon)
	require.Len(t, update.Data.Epoch.ActiveSet, expAsSize)
}

func TestGenerator_Generate(t *testing.T) {
	t.Cleanup(launchServer(t))

	for _, tc := range []struct {
		desc            string
		beacon, actives bool
	}{
		{
			desc:    "bootstrap",
			beacon:  true,
			actives: true,
		},
		{
			desc:   "beacon fallback",
			beacon: true,
		},
		{
			desc:    "actives fallback",
			actives: true,
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodGet, r.Method)
				w.WriteHeader(http.StatusOK)
				var content string
				if strings.HasSuffix(r.URL.String(), "/blocks/782685") {
					content = bitcoinResponse2
				} else {
					content = bitcoinResponse1
				}
				_, err := w.Write([]byte(content))
				require.NoError(t, err)
			}))
			defer ts.Close()

			fs := afero.NewMemMapFs()
			g := NewGenerator(
				ts.URL,
				fmt.Sprintf("%s:%d", target, grpcPort),
				WithLogger(logtest.New(t)),
				WithFilesystem(fs),
				WithHttpClient(ts.Client()),
			)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			targetEpoch := types.EpochID(3)
			persisted, err := g.Generate(ctx, targetEpoch, tc.beacon, tc.actives)
			require.NoError(t, err)

			got, err := afero.ReadFile(fs, persisted)
			require.NoError(t, err)
			require.NotEmpty(t, got)
			if tc.beacon && tc.actives {
				verifyUpdate(t, got, targetEpoch, expectedBeacon, activeSetSize)
			} else if tc.beacon {
				verifyUpdate(t, got, targetEpoch, expectedBeacon, 0)
			} else {
				verifyUpdate(t, got, targetEpoch, emptyBeacon, activeSetSize)
			}
		})
	}
}
