package grpcserver

import (
	"bytes"
	"context"
	"fmt"
	"time"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

// postClient represents a connection to a PoST service.
//
// It uses the grpc interface of the node to send commands to the post service.
// Additionally if instructed it will start the post service and connect it to
// the node.
type postClient struct {
	con chan<- postCommand

	closed chan struct{}
}

func newPostClient(con chan<- postCommand) *postClient {
	return &postClient{
		con:    con,
		closed: make(chan struct{}),
	}
}

func (pc *postClient) Proof(ctx context.Context, challenge []byte) (*types.Post, *types.PostMetadata, error) {
	resp := make(chan *pb.ServiceResponse, 1)
	cmd := postCommand{
		req: &pb.NodeRequest{
			Kind: &pb.NodeRequest_GenProof{
				GenProof: &pb.GenProofRequest{
					Challenge: challenge,
				},
			},
		},
		resp: resp,
	}

	for {
		// send command
		select {
		case <-pc.closed:
			return nil, nil, fmt.Errorf("post client closed")
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case pc.con <- cmd:
		}

		// receive response
		select {
		case <-pc.closed:
			return nil, nil, fmt.Errorf("post client closed")
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case resp := <-resp:
			proofResp := resp.GetGenProof()
			if proofResp == nil {
				return nil, nil, fmt.Errorf("unexpected response of type: %T", resp.GetKind())
			}
			switch proofResp.GetStatus() {
			case pb.GenProofStatus_GEN_PROOF_STATUS_OK:
				if proofResp.GetProof() == nil {
					select {
					case <-ctx.Done():
						return nil, nil, ctx.Err()
					case <-time.After(2 * time.Second):
						// TODO(mafa): make polling interval configurable
						continue
					}
				}

				proof := proofResp.GetProof()
				proofMeta := proofResp.GetMetadata()
				if proofMeta == nil {
					return nil, nil, fmt.Errorf("proof metadata is nil")
				}

				if !bytes.Equal(proofMeta.GetChallenge(), challenge) {
					return nil, nil, fmt.Errorf("unexpected challenge: %x", proofMeta.GetChallenge())
				}

				postMeta := proofMeta.GetMeta()
				if postMeta == nil {
					return nil, nil, fmt.Errorf("post metadata is nil")
				}

				return &types.Post{
						Nonce:   proof.GetNonce(),
						Indices: proof.GetIndices(),
						Pow:     proof.GetPow(),
					}, &types.PostMetadata{
						Challenge:     proofMeta.GetChallenge(),
						LabelsPerUnit: postMeta.GetLabelsPerUnit(),
					}, nil
			case pb.GenProofStatus_GEN_PROOF_STATUS_ERROR:
				return nil, nil, fmt.Errorf("error generating proof: %s", proofResp)
			case pb.GenProofStatus_GEN_PROOF_STATUS_UNSPECIFIED:
				return nil, nil, fmt.Errorf("unspecified error generating proof: %s", proofResp)
			}
		}
	}
}

func (pc *postClient) Close() error {
	select {
	case <-pc.closed:
	default:
		close(pc.closed)
	}

	return nil
}
