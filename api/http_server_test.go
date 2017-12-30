package api

import (
	"fmt"
	"github.com/golang/protobuf/jsonpb"
	config "github.com/spacemeshos/go-spacemesh/api/config"
	pb "github.com/spacemeshos/go-spacemesh/api/pb"
	"github.com/spacemeshos/go-spacemesh/assert"
	"io/ioutil"
	"net/http"
	"strings"
	"testing"
)

func TestJsonServerConfig(t *testing.T) {

	// Arrange
	config.ConfigValues.JsonServerPort = 9031

	// Act
	jsonService := NewJsonHttpServer()

	// Assert
	assert.Equal(t, jsonService.Port, config.ConfigValues.JsonServerPort, "Expected same port")
}

func TestJsonApi(t *testing.T) {

	// Arrange
	const message = "hello world!"
	const contentType = "application/json"

	// Act
	grpcService := NewGrpcService()
	jsonService := NewJsonHttpServer()

	grpcService.StartService()
	jsonService.StartService()

	defer jsonService.Stop()
	defer grpcService.StopService()

	// generate request payload (api input params)
	reqParams := pb.SimpleMessage{Value: message}
	var m jsonpb.Marshaler
	payload, err := m.MarshalToString(&reqParams)
	if err != nil {
		t.Fatalf("m.MarshalToString(%#v) failed with %v; want success", payload, err)
		return
	}

	url := fmt.Sprintf("http://localhost:%d/v1/example/echo", config.ConfigValues.JsonServerPort)
	resp, err := http.Post(url, contentType, strings.NewReader(payload))

	if err != nil {
		t.Errorf("http.Post(%q) failed with %v; want success", url, err)
		return
	}
	defer resp.Body.Close()

	buf, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Errorf("iotuil.ReadAll(resp.Body) failed with %v; want success", err)
		return
	}

	if got, want := resp.StatusCode, http.StatusOK; got != want {
		t.Errorf("resp.StatusCode = %d; want %d", got, want)
		t.Logf("%s", buf)
	}

	var msg pb.SimpleMessage
	if err := jsonpb.UnmarshalString(string(buf), &msg); err != nil {
		t.Errorf("jsonpb.UnmarshalString(%s, &msg) failed with %v; want success", buf, err)
		return
	}
	if got, want := msg.Value, message; got != want {
		t.Errorf("msg.Value = %q; want %q", got, want)
	}

	if value := resp.Header.Get("Content-Type"); value != contentType {
		t.Errorf("Content-Type was %s, wanted %s", value, contentType)
	}
}
