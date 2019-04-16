package nipst

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/merkle-tree"
	"github.com/spacemeshos/poet-ref/rpc"
	"github.com/spacemeshos/poet-ref/rpc/api"
	"github.com/spacemeshos/poet-ref/shared"
	"github.com/spacemeshos/poet-ref/verifier"
	"google.golang.org/grpc"
	"time"
)

type poetRound struct {
	id int
}

type membershipProof struct {
	index int
	root  common.Hash
	proof [][]byte
}

// poetProof can convince a verifier that at least T time must have passed
// from the time the initial member was learned.
type poetProof struct {
	commitment []byte
	n          uint
	proof      *shared.Proof
}

func (p *poetProof) serialize() []byte {
	// TODO(moshababo): implement
	return []byte("")
}

var _ verifyPoetMembershipFunc = verifyPoetMembership

func verifyPoetMembership(member *common.Hash, proof *membershipProof) (bool, error) {
	valid, err := merkle.ValidatePartialTree(
		[]uint64{uint64(proof.index)},
		[][]byte{member[:]},
		proof.proof,
		proof.root[:],
		merkle.GetSha256Parent,
	)

	if err != nil {
		return false, fmt.Errorf("failed to validate merkle proof: %v", err)
	}

	return valid, nil
}

var _ verifyPoetFunc = verifyPoet

func verifyPoet(p *poetProof) (bool, error) {
	v, err := verifier.New(p.commitment, p.n, shared.NewHashFunc(p.commitment))
	if err != nil {
		return false, fmt.Errorf("failed to create a new verifier: %v", err)
	}

	res, err := v.VerifyNIP(*p.proof)
	if err != nil {
		return false, fmt.Errorf("failed to verify proof: %v", err)
	}

	return res, nil
}

var _ verifyPoetMatchesMembershipFunc = verifyPoetMatchesMembership

// verifyPoetMatchesMembership verifies that the poet proof commitment
// is the root in which the membership was proven to.
func verifyPoetMatchesMembership(m *membershipProof, p *poetProof) bool {
	return bytes.Equal(m.root[:], p.commitment)
}

type SeqWorkTicks uint64

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
		return nil, fmt.Errorf("failed to connect to rpc server: %v", err)
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
		return nil, fmt.Errorf("rpc failure: %v", err)
	}

	return &poetRound{id: int(res.RoundId)}, nil
}

func (c *RPCPoetClient) subscribeMembershipProof(r *poetRound,
	challenge common.Hash, timeout time.Duration) (*membershipProof, error) {

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req := api.GetMembershipProofRequest{RoundId: int32(r.id), Challenge: challenge[:], Wait: true}
	res, err := c.client.GetMembershipProof(ctx, &req)
	if err != nil {
		if e := ctx.Err(); e == context.DeadlineExceeded {
			return nil, errors.New("deadline exceeded")
		}
		return nil, fmt.Errorf("rpc failure: %v", err)
	}

	mproof := new(membershipProof)
	mproof.index = int(res.Mproof.Index)
	mproof.proof = res.Mproof.Proof

	// TODO(moshababo): verify length
	copy(mproof.root[:], res.Mproof.Root[:common.HashLength])

	return mproof, nil
}

func (c *RPCPoetClient) subscribeProof(r *poetRound,
	timeout time.Duration) (*poetProof, error) {

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req := api.GetProofRequest{RoundId: int32(r.id), Wait: true}
	res, err := c.client.GetProof(ctx, &req)
	if err != nil {
		if e := ctx.Err(); e == context.DeadlineExceeded {
			return nil, errors.New("deadline exceeded")
		}
		return nil, fmt.Errorf("rpc failure: %v", err)
	}

	labels, err := rpc.WireLabelsToNative(res.Proof.L)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize proof: %v", err)
	}

	p := new(poetProof)
	p.n = uint(res.N)
	p.commitment = res.Commitment
	p.proof = new(shared.Proof)
	p.proof.Phi = res.Proof.Phi
	p.proof.L = *labels

	return p, nil
}
