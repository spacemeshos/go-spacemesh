package activation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/spacemeshos/poet/integration"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

// HTTPPoetHarness utilizes a local self-contained poet server instance
// targeted by an HTTP client, in order to exercise functionality.
type HTTPPoetHarness struct {
	*HTTPPoetClient
	Stdout   io.Reader
	Stderr   io.Reader
	ErrChan  <-chan error
	Teardown func(cleanup bool) error
	h        *integration.Harness
}

// A compile time check to ensure that HTTPPoetClient fully implements PoetProvingServiceClient.
var _ PoetProvingServiceClient = (*HTTPPoetHarness)(nil)

// NewHTTPPoetHarness returns a new instance of HTTPPoetHarness.
func NewHTTPPoetHarness(disableBroadcast bool) (*HTTPPoetHarness, error) {
	cfg, err := integration.DefaultConfig()
	if err != nil {
		return nil, err
	}
	cfg.DisableBroadcast = disableBroadcast
	cfg.Reset = true
	cfg.Duration = "4s"

	h, err := integration.NewHarness(cfg)
	if err != nil {
		return nil, err
	}

	return &HTTPPoetHarness{
		HTTPPoetClient: NewHTTPPoetClient(context.Background(), h.RESTListen()),
		Teardown:       h.TearDown,
		h:              h,
		Stdout:         h.StdoutPipe(),
		Stderr:         h.StderrPipe(),
		ErrChan:        h.ProcessErrors(),
	}, nil
}

// HTTPPoetClient implements PoetProvingServiceClient interface.
type HTTPPoetClient struct {
	baseURL    string
	ctxFactory func() (context.Context, context.CancelFunc)
}

// A compile time check to ensure that HTTPPoetClient fully implements PoetProvingServiceClient.
var _ PoetProvingServiceClient = (*HTTPPoetClient)(nil)

// NewHTTPPoetClient returns new instance of HTTPPoetClient for the specified target.
func NewHTTPPoetClient(ctx context.Context, target string) *HTTPPoetClient {
	return &HTTPPoetClient{
		baseURL: fmt.Sprintf("http://%s/v1", target),
		ctxFactory: func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(ctx, 10*time.Second)
		},
	}
}

// Start is an administrative endpoint of the proving service that tells it to start. This is mostly done in tests,
// since it requires administrative permissions to the proving service.
func (c *HTTPPoetClient) Start(gatewayAddresses []string) error {
	reqBody := StartRequest{GatewayAddresses: gatewayAddresses}
	if err := c.req("POST", "/start", reqBody, nil); err != nil {
		return err
	}

	return nil
}

// Submit registers a challenge in the proving service current open round.
func (c *HTTPPoetClient) Submit(challenge types.Hash32) (*types.PoetRound, error) {
	reqBody := SubmitRequest{Challenge: challenge[:]}
	resBody := &SubmitResponse{}
	if err := c.req("POST", "/submit", reqBody, resBody); err != nil {
		return nil, err
	}

	return &types.PoetRound{ID: resBody.RoundID}, nil
}

// PoetServiceID returns the public key of the PoET proving service.
func (c *HTTPPoetClient) PoetServiceID() ([]byte, error) {
	resBody := &GetInfoResponse{}
	if err := c.req("GET", "/info", nil, resBody); err != nil {
		return nil, err
	}

	return resBody.ServicePubKey, nil
}

func (c *HTTPPoetClient) req(method string, endURL string, reqBody interface{}, resBody interface{}) error {
	jsonReqBody, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("request json marshal failure: %v", err)
	}

	url := fmt.Sprintf("%s%s", c.baseURL, endURL)
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

	if resBody != nil {
		if err := json.NewDecoder(res.Body).Decode(resBody); err != nil {
			return fmt.Errorf("response json decode failure: %v", err)
		}
	}

	return nil
}

// SubmitRequest is the request object for the submit endpoint
type SubmitRequest struct {
	Challenge []byte `json:"challenge,omitempty"`
}

// StartRequest is the request object for the start endpoint
type StartRequest struct {
	GatewayAddresses       []string `json:"gatewayAddresses,omitempty"`
	DisableBroadcast       bool     `json:"disableBroadcast,omitempty"`
	ConnAcksThreshold      int      `json:"connAcksThreshold,omitempty"`
	BroadcastAcksThreshold int      `json:"broadcastAcksThreshold,omitempty"`
}

// SubmitResponse is the response object for the submit endpoint
type SubmitResponse struct {
	RoundID string
}

// GetInfoResponse is the response object for the get-info endpoint
type GetInfoResponse struct {
	OpenRoundID        string
	ExecutingRoundsIDs []string
	ServicePubKey      []byte
}
