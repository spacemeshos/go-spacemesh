package activation_test

import (
	"context"
	"testing"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/signing"
)

type gatewayService struct {
	pb.UnimplementedGatewayServiceServer
}

func hash32FromBytes(b []byte) types.Hash32 {
	hash := types.Hash32{}
	hash.SetBytes(b)
	return hash
}

func (*gatewayService) VerifyChallenge(ctx context.Context, req *pb.VerifyChallengeRequest) (*pb.VerifyChallengeResponse, error) {
	return &pb.VerifyChallengeResponse{
		Hash: hash32FromBytes([]byte("hash")).Bytes(),
	}, nil
}

func TestHTTPPoet(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	r := require.New(t)

	gtw := util.NewMockGrpcServer(t)
	pb.RegisterGatewayServiceServer(gtw.Server, &gatewayService{})
	var eg errgroup.Group
	eg.Go(gtw.Serve)

	poetDir := t.TempDir()
	t.Cleanup(func() { r.NoError(eg.Wait()) })
	t.Cleanup(gtw.Stop)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, err := activation.NewHTTPPoetHarness(ctx, poetDir, activation.WithGateway(gtw.Target()))
	r.NoError(err)
	r.NotNil(c)

	eg.Go(func() error {
		return c.Service.Start(ctx)
	})

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	ch := types.RandomHash()
	poetRound, err := c.Submit(context.Background(), ch.Bytes(), signer.Sign(ch.Bytes()))
	r.NoError(err)
	r.NotNil(poetRound)
	r.Equal(hash32FromBytes([]byte("hash")), poetRound.ChallengeHash)
}
