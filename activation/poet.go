package activation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/poet/rpc/api"
	"io/ioutil"
	"net/http"
	"time"
)

// RPCPoetClient implements PoetProvingServiceClient interface.
type RPCPoetClient struct {
	client   api.PoetClient
	Teardown func(cleanup bool) error
}

// A compile time check to ensure that RPCPoetClient fully implements PoetProvingServiceClient.
var _ PoetProvingServiceClient = (*RPCPoetClient)(nil)

func (c *RPCPoetClient) Start(nodeAddress string) error {
	req := api.StartRequest{NodeAddress: nodeAddress}
	_, err := c.client.Start(context.Background(), &req)
	if err != nil {
		return fmt.Errorf("rpc failure: %v", err)
	}

	return nil
}

func (c *RPCPoetClient) submit(challenge types.Hash32) (*types.PoetRound, error) {
	req := api.SubmitRequest{Challenge: challenge[:]}
	res, err := c.client.Submit(context.Background(), &req)
	if err != nil {
		return nil, fmt.Errorf("rpc failure: %v", err)
	}

	return &types.PoetRound{Id: res.RoundId}, nil
}

func (c *RPCPoetClient) getPoetServiceId() ([]byte, error) {
	req := api.GetInfoRequest{}
	res, err := c.client.GetInfo(context.Background(), &req)
	if err != nil {
		return []byte{}, fmt.Errorf("rpc failure: %v", err)
	}

	return res.ServicePubKey, nil
}

// NewRPCPoetClient returns a new RPCPoetClient instance for the provided
// and already-connected gRPC PoetClient instance.
func NewRPCPoetClient(client api.PoetClient, cleanUp func(cleanup bool) error) *RPCPoetClient {
	return &RPCPoetClient{
		client:   client,
		Teardown: cleanUp,
	}
}

// HTTPPoetClient implements PoetProvingServiceClient interface.
type HTTPPoetClient struct {
	baseUrl    string
	ctxFactory func() (context.Context, context.CancelFunc)
}

// A compile time check to ensure that HTTPPoetClient fully implements PoetProvingServiceClient.
var _ PoetProvingServiceClient = (*HTTPPoetClient)(nil)

// NewHTTPPoetClient returns new instance of HTTPPoetClient for the specified target.
func NewHTTPPoetClient(target string, ctx context.Context) *HTTPPoetClient {
	return &HTTPPoetClient{
		baseUrl: fmt.Sprintf("http://%s/v1", target),
		ctxFactory: func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(ctx, 10*time.Second)
		},
	}
}

func (c *HTTPPoetClient) Start(gatewayAddresses []string) error {
	reqBody := StartRequest{GatewayAddresses: gatewayAddresses}
	if err := c.req("POST", "/start", reqBody, nil); err != nil {
		return err
	}

	return nil
}

func (c *HTTPPoetClient) submit(challenge types.Hash32) (*types.PoetRound, error) {
	reqBody := SubmitRequest{Challenge: challenge[:]}
	resBody := &SubmitResponse{}
	if err := c.req("POST", "/submit", reqBody, resBody); err != nil {
		return nil, err
	}

	return &types.PoetRound{Id: resBody.RoundId}, nil
}

func (c *HTTPPoetClient) getPoetServiceId() ([]byte, error) {
	resBody := &GetInfoResponse{}
	if err := c.req("GET", "/info", nil, resBody); err != nil {
		return nil, err
	}

	return resBody.ServicePubKey, nil
}

func (c *HTTPPoetClient) req(method string, endUrl string, reqBody interface{}, resBody interface{}) error {
	jsonReqBody, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("request json marshal failure: %v", err)
	}

	url := fmt.Sprintf("%s%s", c.baseUrl, endUrl)
	req, err := http.NewRequest(method, url, bytes.NewBuffer(jsonReqBody))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	ctx, cancel := c.ctxFactory()
	defer cancel()
	req.WithContext(ctx)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		data, _ := ioutil.ReadAll(res.Body)
		return fmt.Errorf("response status code: %d, body: %s", res.StatusCode, string(data))
	}

	if err := json.NewDecoder(res.Body).Decode(resBody); err != nil {
		return fmt.Errorf("response json decode failure: %v", err)
	}

	return nil
}

type SubmitRequest struct {
	Challenge []byte `json:"challenge,omitempty"`
}

type StartRequest struct {
	GatewayAddresses       []string `json:"gatewayAddresses,omitempty"`
	DisableBroadcast       bool     `json:"disableBroadcast,omitempty"`
	ConnAcksThreshold      int      `json:"connAcksThreshold,omitempty"`
	BroadcastAcksThreshold int      `json:"broadcastAcksThreshold,omitempty"`
}

type SubmitResponse struct {
	RoundId string
}

type GetInfoResponse struct {
	OpenRoundId        string
	ExecutingRoundsIds []string
	ServicePubKey      []byte
}
