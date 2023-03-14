package miner_test

import (
	"os"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hare/mocks"
	"github.com/spacemeshos/go-spacemesh/miner"
	"github.com/spacemeshos/go-spacemesh/sql"
)

const layersPerEpoch = 3

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(layersPerEpoch)

	res := m.Run()
	os.Exit(res)
}

func TestGradeAtx(t *testing.T) {
	const delta = 10
	for _, tc := range []struct {
		desc      string
		malicious bool
		// distance in second from the epoch start time
		atxReceived, malReceived int
		result                   miner.AtxGrade
	}{
		{
			desc:        "very early atx",
			atxReceived: -41,
			result:      miner.Good,
		},
		{
			desc:        "very early atx, late malfeasance",
			atxReceived: -41,
			malicious:   true,
			malReceived: 0,
			result:      miner.Good,
		},
		{
			desc:        "very early atx, malicious",
			atxReceived: -41,
			malicious:   true,
			malReceived: -10,
			result:      miner.Acceptable,
		},
		{
			desc:        "very early atx, early malicious",
			atxReceived: -41,
			malicious:   true,
			malReceived: -11,
			result:      miner.Evil,
		},
		{
			desc:        "early atx",
			atxReceived: -31,
			result:      miner.Acceptable,
		},
		{
			desc:        "early atx, late malicious",
			atxReceived: -31,
			malicious:   true,
			malReceived: -10,
			result:      miner.Acceptable,
		},
		{
			desc:        "early atx, early malicious",
			atxReceived: -31,
			malicious:   true,
			malReceived: -11,
			result:      miner.Evil,
		},
		{
			desc:        "late atx",
			atxReceived: -30,
			result:      miner.Evil,
		},
		{
			desc:        "very late atx",
			atxReceived: 0,
			result:      miner.Evil,
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			mockMsh := mocks.NewMockmesh(gomock.NewController(t))
			epochStart := time.Now()
			nodeID := types.RandomNodeID()
			if tc.malicious {
				proof := &types.MalfeasanceProof{}
				proof.SetReceived(epochStart.Add(time.Duration(tc.malReceived) * time.Second))
				mockMsh.EXPECT().GetMalfeasanceProof(nodeID).Return(proof, nil)
			} else {
				mockMsh.EXPECT().GetMalfeasanceProof(nodeID).Return(nil, sql.ErrNotFound)
			}
			atxReceived := epochStart.Add(time.Duration(tc.atxReceived) * time.Second)
			got, err := miner.GradeAtx(mockMsh, nodeID, atxReceived, epochStart, delta*time.Second)
			require.NoError(t, err)
			require.Equal(t, tc.result, got)
		})
	}
}
