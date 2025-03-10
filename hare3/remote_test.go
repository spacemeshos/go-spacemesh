package hare3

import (
	"testing"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"
)

func TestRemoteHare(t *testing.T) {
	cfg := Config{
		Committee:       10,
		Leaders:         1,
		IterationsLimit: 5,
		PreroundDelay:   time.Millisecond,
		RoundDuration:   time.Millisecond * 10,
		ProtocolName:    "rh3",
	}
	ctrl := gomock.NewController(t)
	svc := NewMockNodeService(ctrl)
	certifier := NewMockcertifier(ctrl)
	oracle := NewMockoracle(ctrl)
	nodeClock := testNodeClock{
		genesis:       time.Now(),
		layerDuration: time.Minute,
	}

	hare := NewRemoteHare(cfg, &nodeClock, svc, nil, oracle, certifier, zaptest.NewLogger(t))

	s := &session{
		lid:     nodeClock.CurrentLayer(),
		beacon:  types.Beacon{1, 2, 3, 4},
		signers: make([]*signing.EdSigner, 1),
		vrfs:    make([]*types.HareEligibility, 1),
		proto:   newProtocol(10, hare.log.Named("proto")),
	}

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	s.signers[0] = signer

	oracle.EXPECT().CalcEligibility(gomock.Any(), s.lid, gomock.Any(), gomock.Any(), signer.NodeID(), gomock.Any()).Return(10, nil).AnyTimes()

	svc.EXPECT().HareRoundTemplate(gomock.Any(), s.lid, IterRound{
		Iter:  0,
		Round: preround,
	}).Return(&Body{}, nil)
	svc.EXPECT().Publish(gomock.Any(), cfg.ProtocolName, gomock.Any())

	for iter := range uint8(2) {
		svc.EXPECT().HareRoundTemplate(gomock.Any(), s.lid, IterRound{
			Iter:  iter,
			Round: propose,
		}).Return(&Body{}, nil)
		svc.EXPECT().HareRoundTemplate(gomock.Any(), s.lid, IterRound{
			Iter:  iter,
			Round: commit,
		}).Return(&Body{}, nil)
		svc.EXPECT().HareRoundTemplate(gomock.Any(), s.lid, IterRound{
			Iter:  iter,
			Round: notify,
		}).Return(&Body{}, nil)
		svc.EXPECT().Publish(gomock.Any(), cfg.ProtocolName, gomock.Any()).Times(3)
	}

	block := types.BlockID{9, 8, 7, 6}
	svc.EXPECT().BlockID(gomock.Any(), s.lid).Return(block, nil)
	certifier.EXPECT().CertifyBlock(gomock.Any(), signer, s.lid, block, s.beacon).Return(nil)

	hare.run(t.Context(), s)
}
