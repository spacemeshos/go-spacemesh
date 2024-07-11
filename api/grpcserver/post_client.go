package grpcserver

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

// postClient represents a connection to a PoST service.
//
// It uses the grpc interface of the node to send commands to the post service.
// Additionally if instructed it will start the post service and connect it to
// the node.
type postClient struct {
	con           chan<- postCommand
	queryInterval time.Duration

	closed chan struct{}
}

func newPostClient(con chan<- postCommand, queryInterval time.Duration) *postClient {
	return &postClient{
		con:           con,
		queryInterval: queryInterval,
		closed:        make(chan struct{}),
	}
}

func (pc *postClient) Info(ctx context.Context) (*types.PostInfo, error) {
	req := &pb.NodeRequest{
		Kind: &pb.NodeRequest_Metadata{
			Metadata: &pb.MetadataRequest{},
		},
	}
	resp, err := pc.send(ctx, req)
	if err != nil {
		return nil, err
	}
	metadataResp := resp.GetMetadata()
	if metadataResp == nil {
		return nil, fmt.Errorf("unexpected response of type: %T", resp.GetKind())
	}
	meta := metadataResp.GetMeta()
	if meta == nil {
		return nil, errors.New("post metadata is nil")
	}
	var nonce *types.VRFPostIndex
	if meta.Nonce != nil {
		nonce = new(types.VRFPostIndex)
		*nonce = types.VRFPostIndex(meta.GetNonce())
	}
	return &types.PostInfo{
		NodeID:        types.BytesToNodeID(meta.GetNodeId()),
		CommitmentATX: types.BytesToATXID(meta.GetCommitmentAtxId()),
		Nonce:         nonce,

		NumUnits:      meta.GetNumUnits(),
		LabelsPerUnit: meta.GetLabelsPerUnit(),
	}, nil
}

func (pc *postClient) Proof(ctx context.Context, challenge []byte) (*types.Post, *types.PostInfo, error) {
	req := &pb.NodeRequest{
		Kind: &pb.NodeRequest_GenProof{
			GenProof: &pb.GenProofRequest{
				Challenge: challenge,
			},
		},
	}

	var proofResp *pb.GenProofResponse
	for {
		resp, err := pc.send(ctx, req)
		if err != nil {
			return nil, nil, err
		}

		proofResp = resp.GetGenProof()
		if proofResp == nil {
			return nil, nil, fmt.Errorf("unexpected response of type: %T", resp.GetKind())
		}

		switch proofResp.GetStatus() {
		case pb.GenProofStatus_GEN_PROOF_STATUS_ERROR:
			return nil, nil, fmt.Errorf("error generating proof: %s", proofResp)
		case pb.GenProofStatus_GEN_PROOF_STATUS_UNSPECIFIED:
			return nil, nil, fmt.Errorf("unspecified error generating proof: %s", proofResp)
		case pb.GenProofStatus_GEN_PROOF_STATUS_OK:
		default:
			return nil, nil, fmt.Errorf("unknown status: %s", proofResp)
		}

		if proofResp.GetProof() != nil {
			break
		}

		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-time.After(pc.queryInterval):
			continue
		}
	}

	proof := proofResp.GetProof()
	metadata := proofResp.GetMetadata()
	if metadata == nil {
		return nil, nil, errors.New("proof metadata is nil")
	}
	if !bytes.Equal(metadata.GetChallenge(), challenge) {
		return nil, nil, fmt.Errorf("unexpected challenge: %x", metadata.GetChallenge())
	}
	proofMeta := metadata.GetMeta()
	if proofMeta == nil {
		return nil, nil, errors.New("post metadata is nil")
	}
	post := &types.Post{
		Nonce:   proof.GetNonce(),
		Indices: proof.GetIndices(),
		Pow:     proof.GetPow(),
	}
	var nonce *types.VRFPostIndex
	if proofMeta.Nonce != nil {
		nonce = new(types.VRFPostIndex)
		*nonce = types.VRFPostIndex(proofMeta.GetNonce())
	}
	postInfo := &types.PostInfo{
		NodeID:        types.BytesToNodeID(proofMeta.NodeId),
		CommitmentATX: types.BytesToATXID(proofMeta.CommitmentAtxId),
		Nonce:         nonce,

		NumUnits:      proofMeta.GetNumUnits(),
		LabelsPerUnit: proofMeta.GetLabelsPerUnit(),
	}
	return post, postInfo, nil
}

func (pc *postClient) send(ctx context.Context, req *pb.NodeRequest) (*pb.ServiceResponse, error) {
	resp := make(chan *pb.ServiceResponse, 1)
	cmd := postCommand{
		req:  req,
		resp: resp,
	}

	// send command
	select {
	case <-pc.closed:
		return nil, activation.ErrPostClientClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	case pc.con <- cmd:
	}

	// receive response
	select {
	case <-pc.closed:
		return nil, activation.ErrPostClientClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-resp:
		return resp, nil
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
