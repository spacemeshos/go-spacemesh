package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/bootstrap"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
)

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(epochLayers)
	os.Exit(m.Run())
}

const (
	epochLayers    = 3
	activeSetSize  = 11
	expectedBeacon = "8f8a2576" // from bitcoinResponse2
	emptyBeacon    = "00000000"
)

//go:embed bitcoinResponse1.json
var bitcoinResponse1 string

//go:embed bitcoinResponse2.json
var bitcoinResponse2 string

func createAtxs(tb testing.TB, db sql.Executor, epoch types.EpochID, atxids []types.ATXID) {
	for _, id := range atxids {
		atx := &types.ActivationTx{InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PublishEpoch: epoch,
			},
			NumUnits: 1,
		}}
		atx.SetID(id)
		atx.SetEffectiveNumUnits(atx.NumUnits)
		atx.SetReceived(time.Now())
		atx.SmesherID = types.RandomNodeID()
		vAtx, err := atx.Verify(0, 1)
		require.NoError(tb, err)
		require.NoError(tb, atxs.Add(db, vAtx))
	}
}

func launchServer(tb testing.TB, cdb *datastore.CachedDB) (grpcserver.Config, func()) {
	cfg := grpcserver.DefaultTestConfig()
	grpcService := grpcserver.New("127.0.0.1:0", zaptest.NewLogger(tb).Named("grpc"), cfg)
	jsonService := grpcserver.NewJSONHTTPServer("127.0.0.1:0", zaptest.NewLogger(tb).Named("grpc.JSON"))
	s := grpcserver.NewMeshService(cdb, grpcserver.NewMockmeshAPI(gomock.NewController(tb)), nil, nil,
		0, types.Hash20{}, 0, 0, 0)

	pb.RegisterMeshServiceServer(grpcService.GrpcServer, s)
	// start gRPC and json servers
	err := grpcService.Start()
	require.NoError(tb, err)
	err = jsonService.StartService(context.Background(), s)
	require.NoError(tb, err)

	// update config with bound addresses
	cfg.PublicListener = grpcService.BoundAddress
	cfg.JSONListener = jsonService.BoundAddress

	return cfg, func() {
		err := jsonService.Shutdown(context.Background())
		if !errors.Is(err, http.ErrServerClosed) {
			require.NoError(tb, err)
		}
		err = grpcService.Close()
		require.NoError(tb, err)
	}
}

func verifyUpdate(tb testing.TB, data []byte, epoch types.EpochID, expBeacon string, expAsSize int) {
	tb.Helper()
	require.NoError(tb, bootstrap.ValidateSchema(data))

	var update bootstrap.Update
	require.NoError(tb, json.Unmarshal(data, &update))
	require.Equal(tb, SchemaVersion, update.Version)
	require.Equal(tb, epoch.Uint32(), update.Data.Epoch.ID)
	require.Equal(tb, expBeacon, update.Data.Epoch.Beacon)
	require.Len(tb, update.Data.Epoch.ActiveSet, expAsSize)
}

func TestGenerator_Generate(t *testing.T) {
	targetEpoch := types.EpochID(3)
	db := sql.InMemory()
	createAtxs(t, db, targetEpoch-1, types.RandomActiveSet(activeSetSize))
	cfg, cleanup := launchServer(t, datastore.NewCachedDB(db, logtest.New(t)))
	t.Cleanup(cleanup)

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
				cfg.PublicListener,
				WithLogger(logtest.New(t)),
				WithFilesystem(fs),
				WithHttpClient(ts.Client()),
			)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
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
