package nipst

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/spacemeshos/poet/hash"
	"github.com/spacemeshos/poet/rpc/api"
	"github.com/spacemeshos/poet/shared"
	"github.com/spacemeshos/poet/verifier"
	"google.golang.org/grpc"
	"time"
)

type MembershipProof struct {
	Index int
	Root  common.Hash
	Proof [][]byte
}

// poetProof can convince a verifier that at least T time must have passed
// from the time the initial member was learned.
type PoetProof struct {
	Commitment []byte
	N          uint
	Proof      *shared.MerkleProof
}

func (p *PoetProof) serialize() []byte {
	// TODO(moshababo): implement
	return []byte("")
}

var _ verifyPoetFunc = verifyPoet

func verifyPoet(p *PoetProof) (bool, error) {
	leafCount := uint64(1) << p.N
	err := verifier.Validate(*p.Proof, hash.GenLabelHashFunc(p.Commitment), hash.GenMerkleHashFunc(p.Commitment), leafCount, shared.T)
	if err != nil {
		return false, fmt.Errorf("failed to verify proof: %v", err)
	}

	return true, nil
}

var _ verifyPoetMatchesMembershipFunc = verifyPoetMatchesMembership

// verifyPoetMatchesMembership verifies that the poet proof commitment
// is the root in which the membership was proven to.
func verifyPoetMatchesMembership(membershipRoot *common.Hash, poetProof *PoetProof) bool {
	return bytes.Equal(membershipRoot[:], poetProof.Commitment)
}

type SeqWorkTicks uint64

// NewRemoteRPCPoetClient returns a new instance of
// RPCPoetClient for the specified target.
func NewRemoteRPCPoetClient(target string, timeout time.Duration) (*RPCPoetClient, error) {
	conn, err := newClientConn(target, timeout)
	if err != nil {
		return nil, err
	}

	client := api.NewPoetClient(conn)
	cleanUp := func() error {
		return conn.Close()
	}

	return NewRPCPoetClient(client, cleanUp), nil
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
		return nil, fmt.Errorf("failed to connect to rpc server: %v", err)
	}

	return conn, nil
}

// RPCPoetClient implements PoetProvingServiceClient interface.
type RPCPoetClient struct {
	client  api.PoetClient
	CleanUp func() error
}

// NewRPCPoetClient returns a new RPCPoetClient instance for the provided
// and already-connected gRPC PoetClient instance.
func NewRPCPoetClient(client api.PoetClient, cleanUp func() error) *RPCPoetClient {
	return &RPCPoetClient{
		client:  client,
		CleanUp: cleanUp,
	}
}

func (c *RPCPoetClient) id() []byte {
	// TODO(moshababo): implement
	return []byte("id")
}

func (c *RPCPoetClient) submit(challenge common.Hash,
	duration SeqWorkTicks) (*types.PoetRound, error) {

	req := api.SubmitRequest{Challenge: challenge[:]}
	res, err := c.client.Submit(context.Background(), &req)
	if err != nil {
		return nil, fmt.Errorf("rpc failure: %v", err)
	}

	return &types.PoetRound{Id: uint64(res.RoundId)}, nil
}

func (c *RPCPoetClient) subscribeMembershipProof(r *types.PoetRound,
	challenge common.Hash, timeout time.Duration) (*MembershipProof, error) {

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req := api.GetMembershipProofRequest{RoundId: int32(r.Id), Challenge: challenge[:], Wait: true}
	res, err := c.client.GetMembershipProof(ctx, &req)
	if err != nil {
		if e := ctx.Err(); e == context.DeadlineExceeded {
			return nil, errors.New("deadline exceeded")
		}
		return nil, fmt.Errorf("rpc failure: %v", err)
	}

	mproof := new(MembershipProof)
	mproof.Index = int(res.Mproof.Index)
	mproof.Proof = res.Mproof.Proof

	// TODO(moshababo): verify length
	copy(mproof.Root[:], res.Mproof.Root[:common.HashLength])

	return mproof, nil
}

func (c *RPCPoetClient) subscribeProof(r *types.PoetRound,
	timeout time.Duration) (*PoetProof, error) {

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req := api.GetProofRequest{RoundId: int32(r.Id), Wait: true}
	res, err := c.client.GetProof(ctx, &req)
	if err != nil {
		if e := ctx.Err(); e == context.DeadlineExceeded {
			return nil, errors.New("deadline exceeded")
		}
		return nil, fmt.Errorf("rpc failure: %v", err)
	}

	p := new(PoetProof)
	p.N = uint(res.N)
	p.Commitment = res.Commitment
	p.Proof = new(shared.MerkleProof)
	p.Proof.Root = res.Proof.Phi
	p.Proof.ProofNodes = res.Proof.ProofNodes
	p.Proof.ProvenLeaves = res.Proof.ProvenLeaves

	return p, nil
}
