package nipst

import (
	"context"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/poet-ref/rpc/api"
	"github.com/spacemeshos/poet-ref/shared"
	"google.golang.org/grpc"
	"time"
)

type poetRound struct {
	id int
}

type membershipProof struct {
	root        common.Hash
	merkleProof [][]byte
}

func (p *membershipProof) valid() bool {
	// TODO(moshababo): implement
	return true
}

// poetProof can convince a verifier that at least T time must have passed
// from the time the initial challenge was learned.
type poetProof shared.Proof

func (p *poetProof) valid() bool {
	// TODO(moshababo): implement
	return true
}

func (p *poetProof) serialize() []byte {
	// TODO(moshababo): implement
	return []byte(p.Phi)
}

type SeqWorkTicks uint64

type Space uint64

// newRemoteRPCPoetClient returns a new instance of
// RPCPoetClient for the specified target.
func newRemoteRPCPoetClient(target string, timeout time.Duration) (*RPCPoetClient, error) {
	conn, err := newClientConn(target, timeout)
	if err != nil {
		return nil, err
	}

	client := api.NewPoetClient(conn)
	cleanUp := func() error {
		return conn.Close()
	}

	return newRPCPoetClient(client, cleanUp), nil
}

// newClientConn returns a new gRPC client
// connection to the specified target.
func newClientConn(target string, timeout time.Duration) (*grpc.ClientConn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	opts := []grpc.DialOption{
		grpc.WithInsecure(),
		grpc.WithBlock(),
	}
	defer cancel()

	conn, err := grpc.DialContext(ctx, target, opts...)
	if err != nil {
		return nil, fmt.Errorf("unable to connect to RPC server: %v", err)
	}

	return conn, nil
}

// RPCPoetClient implements PoetProvingServiceClient interface.
type RPCPoetClient struct {
	client  api.PoetClient
	CleanUp func() error
}

// newRPCPoetClient returns a new RPCPoetClient instance for the provided
// and already-connected gRPC PoetClient instance.
func newRPCPoetClient(client api.PoetClient, cleanUp func() error) *RPCPoetClient {
	return &RPCPoetClient{
		client:  client,
		CleanUp: cleanUp,
	}
}

func (c *RPCPoetClient) id() string {
	// TODO(moshababo): implement
	return "id"
}

func (c *RPCPoetClient) submit(challenge common.Hash,
	duration SeqWorkTicks) (*poetRound, error) {

	req := api.SubmitRequest{Challenge: challenge[:]}
	res, err := c.client.Submit(context.Background(), &req)
	if err != nil {
		return nil, err
	}

	return &poetRound{id: int(res.RoundId)}, nil
}

func (c *RPCPoetClient) subscribeMembershipProof(r *poetRound,
	challenge common.Hash, timeout time.Duration) (*membershipProof, error) {

	ctx, _ := context.WithTimeout(context.Background(), timeout)
	req := api.GetMembershipProofRequest{RoundId: int32(r.id), Commitment: challenge[:], Wait: true}
	res, err := c.client.GetMembershipProof(ctx, &req)
	if err != nil {
		if e := ctx.Err(); e == context.DeadlineExceeded {
			return nil, errors.New("deadline exceeded")
		}
		return nil, err
	}

	return &membershipProof{merkleProof: res.MerkleProof}, nil
}

func (c *RPCPoetClient) subscribeProof(r *poetRound,
	timeout time.Duration) (*poetProof, error) {

	ctx, _ := context.WithTimeout(context.Background(), timeout)
	req := api.GetProofRequest{RoundId: int32(r.id), Wait: true}

	res, err := c.client.GetProof(ctx, &req)
	if err != nil {
		if e := ctx.Err(); e == context.DeadlineExceeded {
			return nil, errors.New("deadline exceeded")
		}
		return nil, err
	}

	p := &poetProof{
		Phi: res.Proof.Phi,
		L:   wireLabelsToNative(res.Proof.L),
	}

	return p, nil
}

func wireLabelsToNative(in []*api.Labels) (native [shared.T]shared.Labels) {
	for i, inLabels := range in {
		var outLabels shared.Labels
		for _, inLabel := range inLabels.Labels {
			outLabels = append(outLabels, inLabel)
		}
		native[i] = outLabels
	}
	return native
}
