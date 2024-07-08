package grpcserver

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

func launchJsonServer(tb testing.TB, services ...ServiceAPI) (Config, func()) {
	cfg := DefaultTestConfig()

	// run on random port
	jsonService := NewJSONHTTPServer(zaptest.NewLogger(tb).Named("grpc.JSON"), "127.0.0.1:0",
		[]string{}, false)

	// start json server
	require.NoError(tb, jsonService.StartService(context.Background(), services...))

	// update config with bound address
	cfg.JSONListener = jsonService.BoundAddress

	return cfg, func() { assert.NoError(tb, jsonService.Shutdown(context.Background())) }
}

func callEndpoint(ctx context.Context, tb testing.TB, url string, body []byte) ([]byte, int) {
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	require.NoError(tb, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(tb, err)
	require.Equal(tb, "application/json", resp.Header.Get("Content-Type"))
	buf, err := io.ReadAll(resp.Body)
	require.NoError(tb, err)
	require.NoError(tb, resp.Body.Close())
	return buf, resp.StatusCode
}

func TestJsonApi(t *testing.T) {
	const layerDuration = 10 * time.Second
	const layerAvgSize = 10
	const txsPerProposal = 99
	const version = "v0.0.0"
	const build = "cafebabe"

	ctrl, ctx := gomock.WithContext(context.Background(), t)
	peerCounter := NewMockpeerCounter(ctrl)
	meshAPIMock := NewMockmeshAPI(ctrl)
	genTime := NewMockgenesisTimeAPI(ctrl)
	syncer := NewMocksyncer(ctrl)
	conStateAPI := NewMockconservativeState(ctrl)
	svc1 := NewNodeService(peerCounter, meshAPIMock, genTime, syncer, version, build)
	svc2 := NewMeshService(
		datastore.NewCachedDB(statesql.InMemory(), zaptest.NewLogger(t)),
		meshAPIMock,
		conStateAPI,
		genTime,
		5,
		types.Hash20{},
		layerDuration,
		layerAvgSize,
		txsPerProposal,
	)
	cfg, cleanup := launchJsonServer(t, svc1, svc2)
	t.Cleanup(cleanup)
	time.Sleep(time.Second)

	// generate request payload (api input params)
	const message = "hello world!"
	payload, err := protojson.Marshal(&pb.EchoRequest{Msg: &pb.SimpleString{Value: message}})
	require.NoError(t, err)
	respBody, respStatus := callEndpoint(ctx, t, fmt.Sprintf("http://%s/v1/node/echo", cfg.JSONListener), payload)
	require.Equal(t, http.StatusOK, respStatus)
	var msg pb.EchoResponse
	require.NoError(t, protojson.Unmarshal(respBody, &msg))
	require.Equal(t, message, msg.Msg.Value)

	// Test MeshService
	now := time.Now()
	genTime.EXPECT().GenesisTime().Return(now)
	respBody2, respStatus2 := callEndpoint(ctx, t, fmt.Sprintf("http://%s/v1/mesh/genesistime", cfg.JSONListener), nil)
	require.Equal(t, http.StatusOK, respStatus2)
	var msg2 pb.GenesisTimeResponse
	require.NoError(t, protojson.Unmarshal(respBody2, &msg2))
	require.Equal(t, uint64(now.Unix()), msg2.Unixtime.Value)
}
