package nipst

import (
	"context"
	"errors"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/poet-ref/integration"
	"github.com/spacemeshos/poet-ref/rpc/api"
	"github.com/spacemeshos/poet-ref/shared"
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

// RPCPoetHarness is a poet proving service client
// which utilizes a local self-contained poet server instance
// in order to exercise functionality.
type RPCPoetHarness struct {
	h *integration.Harness
}

// newRPCPoetHarness returns a new instance of RPCPoetHarness.
func newRPCPoetHarness() (*RPCPoetHarness, error) {
	h, err := integration.NewHarness()
	if err != nil {
		return nil, err
	}

	return &RPCPoetHarness{h: h}, nil
}

func (c *RPCPoetHarness) id() string {
	// TODO(moshababo): implement
	return "id"
}

func (c *RPCPoetHarness) submit(challenge common.Hash,
	duration SeqWorkTicks) (*poetRound, error) {

	req := api.SubmitRequest{Challenge: challenge[:]}
	res, err := c.h.Submit(context.Background(), &req)
	if err != nil {
		return nil, err
	}

	return &poetRound{id: int(res.RoundId)}, nil
}

func (c *RPCPoetHarness) subscribeMembershipProof(r *poetRound,
	challenge common.Hash, timeout time.Duration) (*membershipProof, error) {

	ctx, _ := context.WithTimeout(context.Background(), timeout)
	req := api.GetMembershipProofRequest{RoundId: int32(r.id), Commitment: challenge[:], Wait: true}
	res, err := c.h.GetMembershipProof(ctx, &req)
	if err != nil {
		if e := ctx.Err(); e == context.DeadlineExceeded {
			return nil, errors.New("deadline exceeded")
		}
		return nil, err
	}

	return &membershipProof{merkleProof: res.MerkleProof}, nil
}

func (c *RPCPoetHarness) subscribeProof(r *poetRound,
	timeout time.Duration) (*poetProof, error) {

	ctx, _ := context.WithTimeout(context.Background(), timeout)
	req := api.GetProofRequest{RoundId: int32(r.id), Wait: true}

	res, err := c.h.GetProof(ctx, &req)
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

func (c *RPCPoetHarness) cleanUp() error {
	return c.h.TearDown()
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
