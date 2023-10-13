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
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func launchJsonServer(tb testing.TB, services ...ServiceAPI) (Config, func()) {
	cfg := DefaultTestConfig()

	// run on random port
	jsonService := NewJSONHTTPServer("127.0.0.1:0", zaptest.NewLogger(tb).Named("grpc.JSON"))

	// start json server
	require.NoError(tb, jsonService.StartService(context.Background(), services...))

	// update config with bound address
	cfg.JSONListener = jsonService.BoundAddress

	return cfg, func() { assert.NoError(tb, jsonService.Shutdown(context.Background())) }
}

func callEndpoint(t *testing.T, url string, payload []byte) ([]byte, int) {
	resp, err := http.Post(url, "application/json", bytes.NewReader(payload))
	require.NoError(t, err)
	require.Equal(t, "application/json", resp.Header.Get("Content-Type"))
	buf, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	return buf, resp.StatusCode
}

func TestJsonApi(t *testing.T) {
	const message = "hello world!"

	ctrl := gomock.NewController(t)
	syncer := NewMocksyncer(ctrl)
	syncer.EXPECT().IsSynced(gomock.Any()).Return(false).AnyTimes()
	peerCounter := NewMockpeerCounter(ctrl)
	genTime := NewMockgenesisTimeAPI(ctrl)
	genesis := time.Unix(genTimeUnix, 0)
	genTime.EXPECT().GenesisTime().Return(genesis)
	svc1 := NewNodeService(peerCounter, meshAPIMock, genTime, syncer, "v0.0.0", "cafebabe")
	svc2 := NewMeshService(datastore.NewCachedDB(sql.InMemory(), logtest.New(t)), meshAPIMock, conStateAPI, genTime, layersPerEpoch, types.Hash20{}, layerDuration, layerAvgSize, txsPerProposal)
	cfg, cleanup := launchJsonServer(t, svc1, svc2)
	t.Cleanup(cleanup)
	time.Sleep(time.Second)

	// generate request payload (api input params)
	payload, err := protojson.Marshal(&pb.EchoRequest{Msg: &pb.SimpleString{Value: message}})
	require.NoError(t, err)
	respBody, respStatus := callEndpoint(t, fmt.Sprintf("http://%s/v1/node/echo", cfg.JSONListener), payload)
	require.Equal(t, http.StatusOK, respStatus)
	var msg pb.EchoResponse
	require.NoError(t, protojson.Unmarshal(respBody, &msg))
	require.Equal(t, message, msg.Msg.Value)

	// Test MeshService
	respBody2, respStatus2 := callEndpoint(t, fmt.Sprintf("http://%s/v1/mesh/genesistime", cfg.JSONListener), nil)
	require.Equal(t, http.StatusOK, respStatus2)
	var msg2 pb.GenesisTimeResponse
	require.NoError(t, protojson.Unmarshal(respBody2, &msg2))
	require.Equal(t, uint64(genesis.Unix()), msg2.Unixtime.Value)
}
