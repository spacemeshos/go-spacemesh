package activation

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/spacemeshos/poet/integration"
	"github.com/spacemeshos/poet/release/proto/go/rpc/api"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

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

// NewHTTPPoetHarness returns a new instance of HTTPPoetHarness.
func NewHTTPPoetHarness(disableBroadcast bool) (*HTTPPoetHarness, error) {
	cfg, err := integration.DefaultConfig()
	if err != nil {
		return nil, fmt.Errorf("default integration config: %w", err)
	}

	cfg.DisableBroadcast = disableBroadcast
	cfg.Reset = true
	cfg.Genesis = time.Now().Add(5 * time.Second)
	cfg.EpochDuration = 4 * time.Second

	ctx, cancel := context.WithDeadline(context.Background(), cfg.Genesis)
	defer cancel()
	h, err := integration.NewHarness(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("new harness: %w", err)
	}

	return &HTTPPoetHarness{
		HTTPPoetClient: NewHTTPPoetClient(h.RESTListen()),
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
	ctxFactory func(ctx context.Context) (context.Context, context.CancelFunc)
}

func defaultPoetClientFunc(target string) PoetProvingServiceClient {
	return NewHTTPPoetClient(target)
}

// NewHTTPPoetClient returns new instance of HTTPPoetClient for the specified target.
func NewHTTPPoetClient(target string) *HTTPPoetClient {
	return &HTTPPoetClient{
		baseURL: fmt.Sprintf("http://%s/v1", target),
		ctxFactory: func(ctx context.Context) (context.Context, context.CancelFunc) {
			return context.WithTimeout(ctx, 10*time.Second)
		},
	}
}

// Start is an administrative endpoint of the proving service that tells it to start. This is mostly done in tests,
// since it requires administrative permissions to the proving service.
func (c *HTTPPoetClient) Start(ctx context.Context, gatewayAddresses []string) error {
	reqBody := api.StartRequest{GatewayAddresses: gatewayAddresses}
	if err := c.req(ctx, "POST", "/start", &reqBody, nil); err != nil {
		return fmt.Errorf("request: %w", err)
	}

	return nil
}

// Submit registers a challenge in the proving service current open round.
func (c *HTTPPoetClient) Submit(ctx context.Context, challenge types.Hash32) (*types.PoetRound, error) {
	reqBody := api.SubmitRequest{Challenge: challenge[:]}
	resBody := api.SubmitResponse{}
	if err := c.req(ctx, "POST", "/submit", &reqBody, &resBody); err != nil {
		return nil, err
	}

	return &types.PoetRound{ID: resBody.RoundId}, nil
}

// PoetServiceID returns the public key of the PoET proving service.
func (c *HTTPPoetClient) PoetServiceID(ctx context.Context) ([]byte, error) {
	resBody := api.GetInfoResponse{}
	if err := c.req(ctx, "GET", "/info", nil, &resBody); err != nil {
		return nil, err
	}

	return resBody.ServicePubKey, nil
}

func (c *HTTPPoetClient) req(ctx context.Context, method string, endURL string, reqBody proto.Message, resBody proto.Message) error {
	jsonReqBody, err := protojson.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("request json marshal failure: %v", err)
	}

	url := fmt.Sprintf("%s%s", c.baseURL, endURL)
	req, err := http.NewRequest(method, url, bytes.NewBuffer(jsonReqBody))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	ctx, cancel := c.ctxFactory(ctx)
	defer cancel()
	req = req.WithContext(ctx)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("perform request: %w", err)
	}
	defer res.Body.Close()

	data, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body (%w)", err)
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("response status code: %d, body: %s", res.StatusCode, string(data))
	}

	if resBody != nil {
		if err := protojson.Unmarshal(data, resBody); err != nil {
			return fmt.Errorf("response json decode failure: %v", err)
		}
	}

	return nil
}
