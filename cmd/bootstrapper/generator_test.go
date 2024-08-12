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
		atx := &types.ActivationTx{
			PublishEpoch: epoch,
			NumUnits:     1,
			TickCount:    1,
			SmesherID:    types.RandomNodeID(),
		}
		atx.SetID(id)
		atx.SetReceived(time.Now())
		require.NoError(tb, atxs.Add(db, atx, types.AtxBlob{}))
	}
}

func launchServer(tb testing.TB, cdb *datastore.CachedDB) (grpcserver.Config, func()) {
	cfg := grpcserver.DefaultTestConfig()
	grpcService := grpcserver.New("127.0.0.1:0", zaptest.NewLogger(tb).Named("grpc"), cfg)
	jsonService := grpcserver.NewJSONHTTPServer(zaptest.NewLogger(tb).Named("grpc.JSON"), "127.0.0.1:0",
		[]string{}, false)
	s := grpcserver.NewMeshService(cdb, grpcserver.NewMockmeshAPI(gomock.NewController(tb)), nil, nil,
		0, types.Hash20{}, 0, 0, 0)

	pb.RegisterMeshServiceServer(grpcService.GrpcServer, s)
	// start gRPC and json servers
	err := grpcService.Start()
	require.NoError(tb, err)
	err = jsonService.StartService(s)
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
	t.Parallel()
	targetEpoch := types.EpochID(3)
	db := sql.InMemory()
	createAtxs(t, db, targetEpoch-1, types.RandomActiveSet(activeSetSize))
	cfg, cleanup := launchServer(t, datastore.NewCachedDB(db, zaptest.NewLogger(t)))
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
			t.Parallel()
			mux := http.NewServeMux()
			mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.String(), "/blocks/782685") {
					w.Write([]byte(bitcoinResponse2))
				} else {
					w.Write([]byte(bitcoinResponse1))
				}
			})
			ts := httptest.NewServer(mux)
			defer ts.Close()

			fs := afero.NewMemMapFs()
			g := NewGenerator(
				ts.URL,
				cfg.PublicListener,
				WithLogger(zaptest.NewLogger(t)),
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

func TestGenerator_CheckAPI(t *testing.T) {
	t.Parallel()
	targetEpoch := types.EpochID(3)
	db := sql.InMemory()
	lg := zaptest.NewLogger(t)
	createAtxs(t, db, targetEpoch-1, types.RandomActiveSet(activeSetSize))
	cfg, cleanup := launchServer(t, datastore.NewCachedDB(db, lg))
	t.Cleanup(cleanup)

	fs := afero.NewMemMapFs()
	g := NewGenerator(
		"https://api.blockcypher.com/v1/btc/main",
		cfg.PublicListener,
		WithLogger(lg),
		WithFilesystem(fs),
		WithHttpClient(http.DefaultClient),
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	persisted, err := g.Generate(ctx, targetEpoch, true, false)
	require.NoError(t, err)

	got, err := afero.ReadFile(fs, persisted)
	require.NoError(t, err)
	require.NotEmpty(t, got)

	require.NoError(t, bootstrap.ValidateSchema(got))

	var update bootstrap.Update
	require.NoError(t, json.Unmarshal(got, &update))
	require.NotEqual(t, "00000000", update.Data.Epoch.Beacon)
}
