package ballots

import (
	"flag"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"github.com/DataDog/zstd"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
)

const layersPerEpoch = 3

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(layersPerEpoch)

	res := m.Run()
	os.Exit(res)
}

func TestLayer(t *testing.T) {
	db := sql.InMemory()
	start := types.LayerID(1)
	pub := types.BytesToNodeID([]byte{1, 1, 1})
	ballots := []types.Ballot{
		types.NewExistingBallot(types.BallotID{1}, types.EmptyEdSignature, pub, start),
		types.NewExistingBallot(types.BallotID{2}, types.EmptyEdSignature, pub, start),
	}
	for _, ballot := range ballots {
		require.NoError(t, Add(db, &ballot))
	}

	ids, err := IDsInLayer(db, start)
	require.NoError(t, err)
	require.Len(t, ids, len(ballots))

	rst, err := Layer(db, start)
	require.NoError(t, err)
	require.Len(t, rst, len(ballots))
	for i, ballot := range rst {
		require.Equal(t, &ballots[i], ballot)
	}

	require.NoError(t, identities.SetMalicious(db, pub, []byte("proof"), time.Now()))
	rst, err = Layer(db, start)
	require.NoError(t, err)
	require.Len(t, rst, len(ballots))
	for _, ballot := range rst {
		require.True(t, ballot.IsMalicious())
	}

	rst, err = LayerNoMalicious(db, start)
	require.NoError(t, err)
	require.Len(t, rst, len(ballots))
	for _, ballot := range rst {
		require.False(t, ballot.IsMalicious())
	}
}

func TestAdd(t *testing.T) {
	db := sql.InMemory()
	nodeID := types.RandomNodeID()
	ballot := types.NewExistingBallot(types.BallotID{1}, types.RandomEdSignature(), nodeID, types.LayerID(0))
	_, err := Get(db, ballot.ID())
	require.ErrorIs(t, err, sql.ErrNotFound)

	require.NoError(t, Add(db, &ballot))
	require.ErrorIs(t, Add(db, &ballot), sql.ErrObjectExists)

	stored, err := Get(db, ballot.ID())
	require.NoError(t, err)
	require.Equal(t, &ballot, stored)

	require.NoError(t, identities.SetMalicious(db, nodeID, []byte("proof"), time.Now()))
	stored, err = Get(db, ballot.ID())
	require.NoError(t, err)
	require.True(t, stored.IsMalicious())
}

func TestUpdateBlob(t *testing.T) {
	db := sql.InMemory()
	nodeID := types.RandomNodeID()
	ballot := types.NewExistingBallot(types.BallotID{1}, types.RandomEdSignature(), nodeID, types.LayerID(0))
	ballot.EpochData = &types.EpochData{
		ActiveSetHash: types.RandomHash(),
	}
	ballot.ActiveSet = types.RandomActiveSet(199)
	require.NoError(t, Add(db, &ballot))
	got, err := Get(db, types.BallotID{1})
	require.NoError(t, err)
	require.Equal(t, ballot, *got)

	ballot.ActiveSet = nil
	require.NoError(t, UpdateBlob(db, types.BallotID{1}, codec.MustEncode(&ballot)))
	got, err = Get(db, types.BallotID{1})
	require.NoError(t, err)
	require.Empty(t, got.ActiveSet)
}

func TestHas(t *testing.T) {
	db := sql.InMemory()
	ballot := types.NewExistingBallot(types.BallotID{1}, types.EmptyEdSignature, types.EmptyNodeID, types.LayerID(0))

	exists, err := Has(db, ballot.ID())
	require.NoError(t, err)
	require.False(t, exists)

	require.NoError(t, Add(db, &ballot))
	exists, err = Has(db, ballot.ID())
	require.NoError(t, err)
	require.True(t, exists)
}

func TestLatest(t *testing.T) {
	db := sql.InMemory()
	latest, err := LatestLayer(db)
	require.NoError(t, err)
	require.Equal(t, types.LayerID(0), latest)

	ballot := types.NewExistingBallot(types.BallotID{1}, types.EmptyEdSignature, types.EmptyNodeID, types.LayerID(11))
	require.NoError(t, Add(db, &ballot))
	latest, err = LatestLayer(db)
	require.NoError(t, err)
	require.Equal(t, ballot.Layer, latest)

	newBallot := types.NewExistingBallot(
		types.BallotID{2},
		types.EmptyEdSignature,
		types.EmptyNodeID,
		types.LayerID(12),
	)
	require.NoError(t, Add(db, &newBallot))
	latest, err = LatestLayer(db)
	require.NoError(t, err)
	require.Equal(t, newBallot.Layer, latest)
}

func TestLayerBallotBySmesher(t *testing.T) {
	db := sql.InMemory()
	lid := types.LayerID(1)
	nodeID1 := types.RandomNodeID()
	nodeID2 := types.RandomNodeID()
	ballots := []types.Ballot{
		types.NewExistingBallot(types.BallotID{2}, types.EmptyEdSignature, nodeID1, lid.Add(1)),
		types.NewExistingBallot(types.BallotID{3}, types.EmptyEdSignature, nodeID2, lid),
	}
	for _, ballot := range ballots {
		require.NoError(t, Add(db, &ballot))
	}

	prev, err := LayerBallotByNodeID(db, lid, nodeID1)
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.Nil(t, prev)
	prev, err = LayerBallotByNodeID(db, lid, nodeID2)
	require.NoError(t, err)
	require.NotNil(t, prev)
	require.Equal(t, ballots[1], *prev)
}

func newAtx(signer *signing.EdSigner, layerID types.LayerID) (*types.VerifiedActivationTx, error) {
	atx := &types.ActivationTx{
		InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PublishEpoch: layerID.GetEpoch(),
				PrevATXID:    types.RandomATXID(),
			},
			NumUnits: 2,
		},
	}

	nodeID := signer.NodeID()
	atx.SetID(types.ATXID{1, 2, 3})
	atx.SmesherID = nodeID
	atx.SetEffectiveNumUnits(atx.NumUnits)
	atx.SetReceived(time.Now().Local())
	return atx.Verify(0, 1)
}

func TestFirstInEpoch(t *testing.T) {
	db := sql.InMemory()
	lid := types.LayerID(layersPerEpoch * 2)
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	atx, err := newAtx(sig, lid)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(db, atx))

	got, err := FirstInEpoch(db, atx.ID(), 2)
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.Nil(t, got)

	b1 := types.NewExistingBallot(types.BallotID{1}, types.EmptyEdSignature, sig.NodeID(), lid)
	b1.AtxID = atx.ID()
	require.NoError(t, Add(db, &b1))
	b2 := types.NewExistingBallot(types.BallotID{2}, types.EmptyEdSignature, sig.NodeID(), lid)
	b2.AtxID = atx.ID()
	b2.EpochData = &types.EpochData{}
	require.NoError(t, Add(db, &b2))
	b3 := types.NewExistingBallot(types.BallotID{3}, types.EmptyEdSignature, sig.NodeID(), lid.Add(1))
	b3.AtxID = atx.ID()
	require.NoError(t, Add(db, &b3))

	got, err = FirstInEpoch(db, atx.ID(), 2)
	require.NoError(t, err)
	require.False(t, got.IsMalicious())
	require.Equal(t, got.AtxID, atx.ID())
	require.Equal(t, got.ID(), b1.ID())

	last, err := LastInEpoch(db, atx.ID(), 2)
	require.NoError(t, err)
	require.Equal(t, b3.ID(), last.ID())

	require.NoError(t, identities.SetMalicious(db, sig.NodeID(), []byte("bad"), time.Now()))
	got, err = FirstInEpoch(db, atx.ID(), 2)
	require.NoError(t, err)
	require.True(t, got.IsMalicious())
	require.Equal(t, got.AtxID, atx.ID())
	require.Equal(t, got.ID(), b1.ID())
}

func TestAllFirstInEpoch(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		desc    string
		target  types.EpochID
		ballots []types.Ballot
		expect  []int // references to ballots field
	}{
		{
			"sanity",
			0,
			[]types.Ballot{
				types.NewExistingBallot(
					types.BallotID{1}, types.EmptyEdSignature, types.NodeID{1},
					types.EpochID(0).FirstLayer()+2,
				),
				types.NewExistingBallot(
					types.BallotID{2}, types.EmptyEdSignature, types.NodeID{1},
					types.EpochID(0).FirstLayer(),
				),
			},
			[]int{1},
		},
		{
			"multiple smeshers",
			0,
			[]types.Ballot{
				types.NewExistingBallot(
					types.BallotID{1}, types.EmptyEdSignature, types.NodeID{1},
					types.EpochID(0).FirstLayer()+2,
				),
				types.NewExistingBallot(
					types.BallotID{2}, types.EmptyEdSignature, types.NodeID{1},
					types.EpochID(0).FirstLayer(),
				),
				types.NewExistingBallot(
					types.BallotID{3}, types.EmptyEdSignature, types.NodeID{2},
					types.EpochID(1).FirstLayer()-1,
				),
				types.NewExistingBallot(
					types.BallotID{4}, types.EmptyEdSignature, types.NodeID{2},
					types.EpochID(0).FirstLayer(),
				),
			},
			[]int{1, 3},
		},
		{
			"empty",
			1,
			[]types.Ballot{
				types.NewExistingBallot(
					types.BallotID{1}, types.EmptyEdSignature, types.NodeID{1},
					types.EpochID(0).FirstLayer(),
				),
				types.NewExistingBallot(
					types.BallotID{3}, types.EmptyEdSignature, types.NodeID{2},
					types.EpochID(0).FirstLayer(),
				),
			},
			[]int{},
		},
		{
			"multi epoch",
			1,
			[]types.Ballot{
				types.NewExistingBallot(
					types.BallotID{1}, types.EmptyEdSignature, types.NodeID{1},
					types.EpochID(0).FirstLayer(),
				),
				types.NewExistingBallot(
					types.BallotID{3}, types.EmptyEdSignature, types.NodeID{2},
					types.EpochID(0).FirstLayer(),
				),
				types.NewExistingBallot(
					types.BallotID{4}, types.EmptyEdSignature, types.NodeID{1},
					types.EpochID(1).FirstLayer(),
				),
				types.NewExistingBallot(
					types.BallotID{5}, types.EmptyEdSignature, types.NodeID{2},
					types.EpochID(1).FirstLayer(),
				),
			},
			[]int{2, 3},
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			db := sql.InMemory()
			for _, ballot := range tc.ballots {
				require.NoError(t, Add(db, &ballot))
			}
			var expect []*types.Ballot
			for _, bi := range tc.expect {
				expect = append(expect, &tc.ballots[bi])
			}
			results, err := AllFirstInEpoch(db, tc.target)
			require.NoError(t, err)
			require.Equal(t, expect, results)
		})
	}
}

func TestEncodingSize(t *testing.T) {
	ballot := types.Ballot{}
	fmt.Println("empty", len(codec.MustEncode(&ballot)))
	ballot.EpochData = &types.EpochData{}
	fmt.Println("with epoch data", len(codec.MustEncode(&ballot)))
	ballot.EligibilityProofs = []types.VotingEligibility{{}}
	fmt.Println("single eligibility", len(codec.MustEncode(&ballot)))
	ballot.Votes = types.Votes{Support: []types.Vote{{ID: types.BlockID{1}}}}
	fmt.Println("single support", len(codec.MustEncode(&ballot)))
}

var (
	dbpath = flag.String("dbpath", "", "path to database")
)

func TestCompressions(t *testing.T) {
	// chunk size 5000 compressed 949777 uncompressed 1754679 ratio 0.5412824795874345 duration 57.337931ms level 5
	// chunk size 5000 compressed 946271 uncompressed 1806652 ratio 0.5237704881737048 duration 57.609862ms level 5
	// chunk size 5000 compressed 955630 uncompressed 1908897 ratio 0.5006189438193889 duration 54.430003ms level 5
	// chunk size 5000 compressed 956587 uncompressed 2029065 ratio 0.471442265279821 duration 54.258789ms level 5
	// chunk size 5000 compressed 959445 uncompressed 2079847 ratio 0.4613055671883557 duration 55.865252ms level 5

	if len(*dbpath) == 0 {
		t.Skip("set -dbpath=<PATH> to run this test")
	}

	db, err := sql.Open("file:" + *dbpath)
	require.NoError(t, err)
	defer db.Close()

	i := 0
	input := make([]byte, 0, 10<<20)
	output := make([]byte, 0, 10<<20)
	level := zstd.DefaultCompression
	require.NoError(t, IterateBlobs(db, func(id types.BallotID, reaader io.Reader) bool {
		i++
		buf, _ := io.ReadAll(reaader)
		input = append(input, buf...)
		if i == 5000 {
			start := time.Now()
			output, err = zstd.CompressLevel(output, input, level)
			require.NoError(t, err)
			fmt.Println(
				"chunk size",
				i,
				"compressed",
				len(output),
				"uncompressed",
				len(input),
				"ratio",
				float64(len(output))/float64(len(input)),
				"duration",
				time.Since(start),
				"level",
				level,
			)
			i = 0
			input = input[:0]
			output = output[:0]
		}
		return true
	}))
}
