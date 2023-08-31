package proposals

import (
	"context"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
)

func gatx(id types.ATXID, epoch types.EpochID, smesher types.NodeID, units uint32, nonce types.VRFPostIndex) types.VerifiedActivationTx {
	atx := &types.ActivationTx{}
	atx.NumUnits = units
	atx.PublishEpoch = epoch
	atx.SmesherID = smesher
	atx.SetID(id)
	atx.SetEffectiveNumUnits(atx.NumUnits)
	atx.SetReceived(time.Time{}.Add(1))
	atx.VRFNonce = &nonce
	verified, err := atx.Verify(0, 100)
	if err != nil {
		panic(err)
	}
	return *verified
}

func gatxZeroHeight(id types.ATXID, epoch types.EpochID, smesher types.NodeID, units uint32, nonce types.VRFPostIndex) types.VerifiedActivationTx {
	atx := &types.ActivationTx{}
	atx.NumUnits = units
	atx.PublishEpoch = epoch
	atx.SmesherID = smesher
	atx.SetID(id)
	atx.SetEffectiveNumUnits(atx.NumUnits)
	atx.SetReceived(time.Time{}.Add(1))
	atx.VRFNonce = &nonce
	verified, err := atx.Verify(0, 0)
	if err != nil {
		panic(err)
	}
	return *verified
}

func gatxNilNonce(id types.ATXID, epoch types.EpochID, smesher types.NodeID, units uint32) types.VerifiedActivationTx {
	atx := &types.ActivationTx{}
	atx.NumUnits = units
	atx.PublishEpoch = epoch
	atx.SmesherID = smesher
	atx.SetID(id)
	atx.SetEffectiveNumUnits(atx.NumUnits)
	atx.SetReceived(time.Time{}.Add(1))
	verified, err := atx.Verify(0, 100)
	if err != nil {
		panic(err)
	}
	return *verified
}

func gdata(slots uint32, beacon types.Beacon) *types.EpochData {
	return &types.EpochData{
		Beacon:           beacon,
		EligibilityCount: slots,
	}
}

func gactiveset(atxs ...types.ATXID) []types.ATXID {
	return atxs
}

func geligibilities(js ...uint32) (rst []types.VotingEligibility) {
	for _, j := range js {
		rst = append(rst, types.VotingEligibility{J: j})
	}
	return rst
}

// geligibilityWithSig is useful to influence CalcEligibleLayer.
func geligibilityWithSig(j uint32, sig string) []types.VotingEligibility {
	el := types.VotingEligibility{J: j}
	copy(el.Sig[:], sig)
	return []types.VotingEligibility{el}
}

func gballot(id types.BallotID, atxid types.ATXID, activeset []types.ATXID, smesher types.NodeID, layer types.LayerID,
	edata *types.EpochData, eligibilities []types.VotingEligibility,
) types.Ballot {
	ballot := types.Ballot{}
	ballot.Layer = layer
	ballot.EpochData = edata
	ballot.AtxID = atxid
	ballot.EligibilityProofs = eligibilities
	ballot.ActiveSet = activeset
	ballot.SmesherID = smesher
	ballot.SetID(id)
	return ballot
}

func gref(id types.BallotID, atxid types.ATXID, smesher types.NodeID, layer types.LayerID,
	ref types.BallotID,
	eligibilities []types.VotingEligibility,
) types.Ballot {
	ballot := types.Ballot{}
	ballot.Layer = layer
	ballot.RefBallot = types.BallotID(ref)
	ballot.AtxID = atxid
	ballot.EligibilityProofs = eligibilities
	ballot.SmesherID = smesher
	ballot.SetID(id)
	return ballot
}

func TestEligibilityValidator(t *testing.T) {
	types.SetLayersPerEpoch(layersPerEpoch)

	epoch := types.EpochID(4)
	publish := epoch - 1

	for _, tc := range []struct {
		desc      string
		current   types.LayerID
		minWeight uint64
		atxs      []types.VerifiedActivationTx
		ballots   []types.Ballot
		vrfFailed bool
		executed  types.Ballot
		fail      bool
		err       string
	}{
		{
			desc:    "ref ballot in current",
			current: epoch.FirstLayer(),
			atxs: []types.VerifiedActivationTx{
				gatx(types.ATXID{1}, publish, types.NodeID{1}, 10, 10),
				gatx(types.ATXID{2}, publish, types.NodeID{2}, 10, 10),
			},
			executed: gballot(
				types.BallotID{1}, types.ATXID{1}, gactiveset(types.ATXID{1}, types.ATXID{2}),
				types.NodeID{1}, epoch.FirstLayer(), gdata(15, types.Beacon{1}),
				geligibilities(1, 2),
			),
		},
		{
			desc:      "ref ballot in current low activeset",
			current:   epoch.FirstLayer(),
			minWeight: 10000,
			atxs: []types.VerifiedActivationTx{
				gatx(types.ATXID{1}, publish, types.NodeID{1}, 10, 10),
				gatx(types.ATXID{2}, publish, types.NodeID{2}, 10, 10),
			},
			executed: gballot(
				types.BallotID{1}, types.ATXID{1}, gactiveset(types.ATXID{1}, types.ATXID{2}),
				types.NodeID{1}, epoch.FirstLayer(), gdata(3, types.Beacon{1}),
				geligibilities(1, 2),
			),
		},
		{
			desc:    "ref ballot zero height",
			current: epoch.FirstLayer(),
			atxs: []types.VerifiedActivationTx{
				gatxZeroHeight(types.ATXID{1}, publish, types.NodeID{1}, 10, 10),
			},
			executed: gballot(
				types.BallotID{1}, types.ATXID{1}, gactiveset(types.ATXID{1}),
				types.NodeID{1}, epoch.FirstLayer(), gdata(15, types.Beacon{1}),
				geligibilities(1, 2),
			),
			fail: true,
			err:  "zero total weight",
		},
		{
			desc:    "ref ballot in previous",
			current: epoch.FirstLayer(),
			atxs: []types.VerifiedActivationTx{
				gatx(types.ATXID{1}, publish-1, types.NodeID{1}, 10, 10),
			},
			executed: gballot(
				types.BallotID{1}, types.ATXID{1}, nil,
				types.NodeID{1}, publish.FirstLayer(), gdata(15, types.Beacon{1}),
				geligibilities(1, 2),
			),
		},
		{
			desc: "no eligibilities",
			executed: gballot(
				types.BallotID{1}, types.ATXID{1}, nil,
				types.NodeID{1}, publish.FirstLayer(), gdata(15, types.Beacon{1}),
				nil,
			),
			fail: true,
			err:  "empty eligibility list",
		},
		{
			desc: "no atx",
			executed: gballot(
				types.BallotID{1}, types.ATXID{1}, nil,
				types.NodeID{1}, publish.FirstLayer(), gdata(15, types.Beacon{1}),
				geligibilities(1, 2),
			),
			fail: true,
			err:  "failed to load atx",
		},
		{
			desc:    "ref ballot in secondary no epoch data",
			current: epoch.FirstLayer(),
			atxs: []types.VerifiedActivationTx{
				gatx(types.ATXID{1}, publish-1, types.NodeID{1}, 10, 10),
			},
			executed: gballot(
				types.BallotID{1}, types.ATXID{1}, nil,
				types.NodeID{1}, publish.FirstLayer(), nil,
				geligibilities(1, 2),
			),
			fail: true,
			err:  "epoch data is missing",
		},
		{
			desc:    "ref ballot in current no epoch data",
			current: epoch.FirstLayer(),
			atxs: []types.VerifiedActivationTx{
				gatx(types.ATXID{1}, epoch-1, types.NodeID{1}, 10, 10),
			},
			executed: gballot(
				types.BallotID{1}, types.ATXID{1}, nil,
				types.NodeID{1}, epoch.FirstLayer(), nil,
				geligibilities(1, 2),
			),
			fail: true,
			err:  "epoch data is missing",
		},
		{
			desc:    "ref ballot in current activeset empty",
			current: epoch.FirstLayer(),
			atxs: []types.VerifiedActivationTx{
				gatx(types.ATXID{1}, epoch-1, types.NodeID{1}, 10, 10),
			},
			executed: gballot(
				types.BallotID{1}, types.ATXID{1}, nil,
				types.NodeID{1}, epoch.FirstLayer(), gdata(10, types.Beacon{1}),
				geligibilities(1, 2),
			),
			fail: true,
			err:  "empty active set",
		},
		{
			desc:    "ref ballot in current empty beacon",
			current: epoch.FirstLayer(),
			atxs: []types.VerifiedActivationTx{
				gatx(types.ATXID{1}, epoch-1, types.NodeID{1}, 10, 10),
			},
			executed: gballot(
				types.BallotID{1}, types.ATXID{1}, nil,
				types.NodeID{1}, epoch.FirstLayer(), gdata(10, types.EmptyBeacon),
				geligibilities(1, 2),
			),
			fail: true,
			err:  "beacon is missing",
		},
		{
			desc:    "ref ballot in current atx in set missing",
			current: epoch.FirstLayer(),
			atxs: []types.VerifiedActivationTx{
				gatx(types.ATXID{1}, epoch-1, types.NodeID{1}, 10, 10),
			},
			executed: gballot(
				types.BallotID{1}, types.ATXID{1}, gactiveset(types.ATXID{1}, types.ATXID{2}),
				types.NodeID{1}, epoch.FirstLayer(), gdata(10, types.Beacon{1}),
				geligibilities(1, 2),
			),
			fail: true,
			err:  "atx in active set is missing",
		},
		{
			desc:    "mismatched num eligible",
			current: epoch.FirstLayer(),
			atxs: []types.VerifiedActivationTx{
				gatx(types.ATXID{1}, epoch-1, types.NodeID{1}, 10, 0),
			},
			executed: gballot(
				types.BallotID{1}, types.ATXID{1}, gactiveset(types.ATXID{1}),
				types.NodeID{1}, epoch.FirstLayer(), gdata(10, types.Beacon{1}),
				geligibilities(1, 2),
			),
			fail: true,
			err:  "ballot has incorrect eligibility count",
		},
		{
			desc:    "ballot targets wrong epoch",
			current: epoch.FirstLayer(),
			atxs: []types.VerifiedActivationTx{
				gatx(types.ATXID{1}, epoch-1, types.NodeID{1}, 10, 0),
			},
			executed: gballot(
				types.BallotID{1}, types.ATXID{1}, gactiveset(types.ATXID{1}),
				types.NodeID{1}, (epoch + 1).FirstLayer(), gdata(10, types.Beacon{1}),
				geligibilities(1, 2),
			),
			fail: true,
			err:  "target epoch and ballot publish epoch mismatch",
		},
		{
			desc:    "ballot uses wrong atx",
			current: epoch.FirstLayer(),
			atxs: []types.VerifiedActivationTx{
				gatx(types.ATXID{1}, epoch-1, types.NodeID{2}, 10, 0),
			},
			executed: gballot(
				types.BallotID{1}, types.ATXID{1}, gactiveset(types.ATXID{1}),
				types.NodeID{1}, epoch.FirstLayer(), gdata(10, types.Beacon{1}),
				geligibilities(1, 2),
			),
			fail: true,
			err:  "ballot smesher key and ATX node key mismatch",
		},
		{
			desc:    "no vrf nonce",
			current: epoch.FirstLayer(),
			atxs: []types.VerifiedActivationTx{
				gatxNilNonce(types.ATXID{1}, epoch-1, types.NodeID{1}, 10),
			},
			executed: gballot(
				types.BallotID{1}, types.ATXID{1}, gactiveset(types.ATXID{1}),
				types.NodeID{1}, epoch.FirstLayer(), gdata(10, types.Beacon{1}),
				geligibilities(1, 2),
			),
			fail: true,
			err:  "no vrf nonce",
		},
		{
			desc:    "secondary ballot",
			current: epoch.FirstLayer(),
			atxs: []types.VerifiedActivationTx{
				gatx(types.ATXID{1}, publish, types.NodeID{1, 1, 1}, 10, 10),
			},
			ballots: []types.Ballot{
				gballot(
					types.BallotID{1}, types.ATXID{1},
					nil,
					types.NodeID{1, 1, 1},
					epoch.FirstLayer(),
					gdata(10, types.Beacon{1}),
					nil,
				),
			},
			executed: gref(
				types.BallotID{2}, types.ATXID{1},
				types.NodeID{1, 1, 1},
				epoch.FirstLayer()+2,
				types.BallotID{1},
				geligibilityWithSig(1, "test1111111"),
			),
		},
		{
			desc:    "secondary ballot empty ref",
			current: epoch.FirstLayer(),
			atxs: []types.VerifiedActivationTx{
				gatx(types.ATXID{1}, publish, types.NodeID{1, 1, 1}, 10, 10),
			},
			executed: gref(
				types.BallotID{2}, types.ATXID{1},
				types.NodeID{1, 1, 1},
				epoch.FirstLayer()+2,
				types.EmptyBallotID,
				geligibilities(1, 2),
			),
			fail: true,
			err:  "epoch data is missing in ref ballot",
		},
		{
			desc:    "secondary ballot missing ref",
			current: epoch.FirstLayer(),
			atxs: []types.VerifiedActivationTx{
				gatx(types.ATXID{1}, publish, types.NodeID{1, 1, 1}, 10, 10),
			},
			executed: gref(
				types.BallotID{2}, types.ATXID{1},
				types.NodeID{1, 1, 1},
				epoch.FirstLayer()+2,
				types.BallotID{1},
				geligibilities(1, 2),
			),
			fail: true,
			err:  "ref ballot is missing",
		},
		{
			desc:    "secondary ballot atx id mismatch",
			current: epoch.FirstLayer(),
			atxs: []types.VerifiedActivationTx{
				gatx(types.ATXID{1}, publish, types.NodeID{1, 1, 1}, 10, 10),
			},
			ballots: []types.Ballot{
				gballot(
					types.BallotID{1}, types.ATXID{2},
					nil,
					types.NodeID{1, 1, 1},
					epoch.FirstLayer(),
					gdata(10, types.Beacon{1}),
					nil,
				),
			},
			executed: gref(
				types.BallotID{2}, types.ATXID{1},
				types.NodeID{1, 1, 1},
				epoch.FirstLayer()+2,
				types.BallotID{1},
				geligibilityWithSig(1, "test1111111"),
			),
			fail: true,
			err:  "sharing atx with a reference ballot",
		},
		{
			desc:    "secondary ballot smesher id mismatch",
			current: epoch.FirstLayer(),
			atxs: []types.VerifiedActivationTx{
				gatx(types.ATXID{1}, publish, types.NodeID{1, 1, 1}, 10, 10),
			},
			ballots: []types.Ballot{
				gballot(
					types.BallotID{1}, types.ATXID{1},
					nil,
					types.NodeID{2, 2, 2},
					epoch.FirstLayer(),
					gdata(10, types.Beacon{1}),
					nil,
				),
			},
			executed: gref(
				types.BallotID{2}, types.ATXID{1},
				types.NodeID{1, 1, 1},
				epoch.FirstLayer()+2,
				types.BallotID{1},
				geligibilityWithSig(1, "test1111111"),
			),
			fail: true,
			err:  "mismatched smesher id with refballot",
		},
		{
			desc:    "secondary ballot mismatched epochs",
			current: epoch.FirstLayer(),
			atxs: []types.VerifiedActivationTx{
				gatx(types.ATXID{1}, publish, types.NodeID{1, 1, 1}, 10, 10),
			},
			ballots: []types.Ballot{
				gballot(
					types.BallotID{1}, types.ATXID{1},
					nil,
					types.NodeID{1, 1, 1},
					(epoch + 1).FirstLayer(),
					gdata(10, types.Beacon{1}),
					nil,
				),
			},
			executed: gref(
				types.BallotID{2}, types.ATXID{1},
				types.NodeID{1, 1, 1},
				epoch.FirstLayer()+2,
				types.BallotID{1},
				geligibilityWithSig(1, "test1111111"),
			),
			fail: true,
			err:  "targets mismatched epoch",
		},
		{
			desc:    "ref ballot bad elig order",
			current: epoch.FirstLayer(),
			atxs: []types.VerifiedActivationTx{
				gatx(types.ATXID{1}, publish, types.NodeID{1}, 10, 10),
				gatx(types.ATXID{2}, publish, types.NodeID{2}, 10, 10),
			},
			executed: gballot(
				types.BallotID{1}, types.ATXID{1}, gactiveset(types.ATXID{1}, types.ATXID{2}),
				types.NodeID{1}, epoch.FirstLayer(), gdata(15, types.Beacon{1}),
				geligibilities(2, 1, 3),
			),
			fail: true,
			err:  "proofs are out of order: 1 <= 2",
		},
		{
			desc:    "proof overflows slots",
			current: epoch.FirstLayer(),
			atxs: []types.VerifiedActivationTx{
				gatx(types.ATXID{1}, publish, types.NodeID{1}, 10, 10),
				gatx(types.ATXID{2}, publish, types.NodeID{2}, 10, 10),
			},
			executed: gballot(
				types.BallotID{1}, types.ATXID{1}, gactiveset(types.ATXID{1}, types.ATXID{2}),
				types.NodeID{1}, epoch.FirstLayer(), gdata(15, types.Beacon{1}),
				geligibilities(15),
			),
			fail: true,
			err:  "proof counter larger than number of slots",
		},
		{
			desc:    "verified didnt pass",
			current: epoch.FirstLayer(),
			atxs: []types.VerifiedActivationTx{
				gatx(types.ATXID{1}, publish, types.NodeID{1}, 10, 10),
				gatx(types.ATXID{2}, publish, types.NodeID{2}, 10, 10),
			},
			executed: gballot(
				types.BallotID{1}, types.ATXID{1}, gactiveset(types.ATXID{1}, types.ATXID{2}),
				types.NodeID{1}, epoch.FirstLayer(), gdata(15, types.Beacon{1}),
				geligibilities(14),
			),
			vrfFailed: true,
			fail:      true,
			err:       "proof contains incorrect VRF signature",
		},
		{
			desc:    "layer is wrong",
			current: epoch.FirstLayer(),
			atxs: []types.VerifiedActivationTx{
				gatx(types.ATXID{1}, publish, types.NodeID{1}, 10, 10),
				gatx(types.ATXID{2}, publish, types.NodeID{2}, 10, 10),
			},
			executed: gballot(
				types.BallotID{1}, types.ATXID{1}, gactiveset(types.ATXID{1}, types.ATXID{2}),
				types.NodeID{1}, epoch.FirstLayer(), gdata(15, types.Beacon{1}),
				geligibilityWithSig(1, "adjust layer"),
			),
			fail: true,
			err:  "ballot has incorrect layer index",
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			ms := fullMockSet(t)
			ms.mclock.EXPECT().CurrentLayer().Return(tc.current).AnyTimes()
			ms.mvrf.EXPECT().Verify(gomock.Any(), gomock.Any(), gomock.Any()).Return(!tc.vrfFailed).AnyTimes()

			lg := logtest.New(t)
			db := datastore.NewCachedDB(sql.InMemory(), lg)
			tv := NewEligibilityValidator(layerAvgSize, layersPerEpoch, tc.minWeight, ms.mclock, ms.md,
				db, ms.mbc, lg, ms.mvrf,
				WithNonceFetcher(db),
			)
			for _, atx := range tc.atxs {
				require.NoError(t, atxs.Add(db, &atx))
			}
			for _, ballot := range tc.ballots {
				require.NoError(t, ballots.Add(db, &ballot))
			}
			if !tc.fail {
				ms.mbc.EXPECT().ReportBeaconFromBallot(tc.executed.Layer.GetEpoch(), &tc.executed, gomock.Any(), gomock.Any())
			}
			rst, err := tv.CheckEligibility(context.Background(), &tc.executed)
			assert.Equal(t, !tc.fail, rst)
			if len(tc.err) == 0 {
				assert.Empty(t, err)
			} else {
				assert.ErrorContains(t, err, tc.err)
			}
		})
	}
}
