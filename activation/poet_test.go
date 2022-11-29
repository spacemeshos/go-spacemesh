package activation_test

import (
	"context"
	"testing"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/stretchr/testify/assert"
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

func (*gatewayService) VerifyChallenge(ctx context.Context, req *pb.VerifyChallengeRequest) (*pb.VerifyChallengeResponse, error) {
	return &pb.VerifyChallengeResponse{
		Hash: []byte("hash"),
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
	t.Cleanup(func() { r.NoError(eg.Wait()) })
	t.Cleanup(gtw.Stop)

	c, err := activation.NewHTTPPoetHarness(true, activation.WithGateway(gtw.Target()))
	r.NoError(err)
	r.NotNil(c)

	t.Cleanup(func() {
		err := c.Teardown(true)
		if assert.NoError(t, err, "failed to tear down harness") {
			t.Log("harness torn down")
		}
	})

	ch := types.RandomHash()
	poetRound, err := c.Submit(context.Background(), ch.Bytes(), signing.NewEdSigner().Sign(ch.Bytes()))
	r.NoError(err)
	r.NotNil(poetRound)
	r.Equal([]byte("hash"), poetRound.ChallengeHash)
}
