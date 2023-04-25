package tortoise

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/tortoise/opinionhash"
)

func TestVotesUpdate(t *testing.T) {
	t.Run("no copies", func(t *testing.T) {
		original := votes{}
		const last = 10
		for i := 0; i < last; i++ {
			original.append(&layerVote{lid: types.LayerID(uint32(i))})
		}
		cp, err := original.update(types.LayerID(last), nil)
		require.NoError(t, err)
		c1 := original.tail
		c2 := cp.tail
		for c1 != nil || c2 != nil {
			require.True(t, c1 == c2, "pointers should be equal")
			c1 = c1.prev
			c2 = c2.prev
		}
	})
	t.Run("copy before last", func(t *testing.T) {
		original := votes{}
		const last = 10
		for i := 0; i < last; i++ {
			original.append(&layerVote{lid: types.LayerID(uint32(i))})
		}
		const modified = last - 2
		cp, err := original.update(types.LayerID(modified), nil)
		require.NoError(t, err)
		c1 := original.tail
		c2 := cp.tail
		for c1 != nil || c2 != nil {
			if c1.lid >= modified {
				require.False(t, c1 == c2)
			} else {
				require.True(t, c1 == c2)
			}
			c1 = c1.prev
			c2 = c2.prev
		}
	})
	t.Run("update abstain", func(t *testing.T) {
		original := votes{}
		const last = 10
		update := map[types.LayerID]map[types.BlockID]headerWithSign{}
		for i := 0; i < last; i++ {
			original.append(&layerVote{
				vote: against,
				lid:  types.LayerID(uint32(i)),
			})
			update[types.LayerID(uint32(i))] = map[types.BlockID]headerWithSign{}
		}
		cp, err := original.update(types.LayerID(0), update)
		require.NoError(t, err)
		for c := original.tail; c != nil; c = c.prev {
			require.Equal(t, against, c.vote)
		}
		for c := cp.tail; c != nil; c = c.prev {
			require.Equal(t, abstain, c.vote)
		}
	})
	t.Run("update blocks", func(t *testing.T) {
		original := votes{}
		const last = 10
		update := map[types.LayerID]map[types.BlockID]headerWithSign{}
		for i := 0; i < last; i++ {
			original.append(&layerVote{
				lid: types.LayerID(uint32(i)),
			})
			update[types.LayerID(uint32(i))] = map[types.BlockID]headerWithSign{
				{byte(i)}: {types.BlockHeader{ID: types.BlockID{byte(i)}}, support},
			}
		}
		cp, err := original.update(types.LayerID(0), update)
		require.NoError(t, err)
		for c := original.tail; c != nil; c = c.prev {
			require.Len(t, c.supported, 0)
		}
		for c := cp.tail; c != nil; c = c.prev {
			require.Len(t, c.supported, 1)
			require.Equal(t, support, c.getVote(c.supported[0]))
		}
	})
}

func TestComputeOpinion(t *testing.T) {
	t.Run("single supported sorted", func(t *testing.T) {
		v := votes{}
		blocks := []*blockInfo{
			{id: types.BlockID{1}, height: 10},
			{id: types.BlockID{2}, height: 5},
		}
		v.append(&layerVote{
			supported: append([]*blockInfo{}, blocks...),
		})
		hh := opinionhash.New()
		hh.WriteSupport(blocks[1].id, blocks[1].height)
		hh.WriteSupport(blocks[0].id, blocks[0].height)
		require.Equal(t, hh.Sum(nil), v.tail.opinion.Bytes())
	})
	t.Run("abstain sentinel", func(t *testing.T) {
		v := votes{}
		v.append(&layerVote{
			vote: abstain,
		})
		hh := opinionhash.New()
		hh.WriteAbstain()
		require.Equal(t, hh.Sum(nil), v.tail.opinion.Bytes())
	})
	t.Run("recursive", func(t *testing.T) {
		v := votes{}
		blocks := []*blockInfo{
			{id: types.BlockID{1}},
			{id: types.BlockID{2}},
		}
		v.append(&layerVote{
			lid:       0,
			supported: []*blockInfo{blocks[0]},
		})
		v.append(&layerVote{
			lid:       1,
			supported: []*blockInfo{blocks[1]},
		})

		hh := opinionhash.New()
		hh.WriteSupport(blocks[0].id, blocks[0].height)
		rst := types.Hash32{}
		hh.Sum(rst[:0])
		hh.Reset()
		hh.WritePrevious(rst)
		hh.WriteSupport(blocks[1].id, blocks[1].height)
		require.Equal(t, hh.Sum(nil), v.tail.opinion.Bytes())
	})
	t.Run("empty layer", func(t *testing.T) {
		v := votes{}
		blocks := []*blockInfo{
			{id: types.BlockID{1}},
		}
		v.append(&layerVote{
			lid:       0,
			supported: []*blockInfo{blocks[0]},
		})
		v.append(&layerVote{
			lid:  1,
			vote: against,
		})

		hh := opinionhash.New()
		hh.WriteSupport(blocks[0].id, blocks[0].height)
		rst := types.Hash32{}
		hh.Sum(rst[:0])
		hh.Reset()
		hh.WritePrevious(rst)
		require.Equal(t, hh.Sum(nil), v.tail.opinion.Bytes())
	})
	t.Run("rehash after update", func(t *testing.T) {
		original := votes{}
		blocks := []*blockInfo{
			{id: types.BlockID{1}},
			{id: types.BlockID{2}},
		}
		updated := types.LayerID(0)
		original.append(&layerVote{
			lid:       updated,
			vote:      against,
			supported: []*blockInfo{blocks[0]},
		})
		original.append(&layerVote{
			lid:       updated.Add(1),
			vote:      against,
			supported: []*blockInfo{blocks[1]},
		})
		v, err := original.update(updated, map[types.LayerID]map[types.BlockID]headerWithSign{
			updated: {blocks[0].id: headerWithSign{blocks[0].header(), against}},
		})
		require.NoError(t, err)
		hh := opinionhash.New()
		rst := types.Hash32{}
		hh.Sum(rst[:0])
		hh.Reset()
		hh.WritePrevious(rst)
		hh.WriteSupport(blocks[1].id, blocks[1].height)
		require.Equal(t, hh.Sum(nil), v.tail.opinion.Bytes())
	})
}

func newTestOpinion(lid types.LayerID) *testOpinion {
	return &testOpinion{lid: lid}
}

type testOpinion struct {
	lid types.LayerID

	sup     []types.BlockHeader
	ag      []types.BlockHeader
	abs     []types.LayerID
	pending struct {
		sup []types.BlockHeader
		ag  []types.BlockHeader
		abs []types.LayerID
	}
	reference votes
}

func (o *testOpinion) support(hid string, height uint64) *testOpinion {
	id := types.BlockID{}
	copy(id[:], hid)
	o.pending.sup = append(o.pending.sup, types.BlockHeader{
		ID:     id,
		Height: height,
		Layer:  o.lid,
	})
	return o
}

func (o *testOpinion) against(hid string, height uint64) *testOpinion {
	id := types.BlockID{}
	copy(id[:], hid)
	o.pending.ag = append(o.pending.ag, types.BlockHeader{
		ID:     id,
		Height: height,
		Layer:  o.lid,
	})
	return o
}

func (o *testOpinion) abstain() *testOpinion {
	o.pending.abs = append(o.pending.abs, o.lid)
	return o
}

func (o *testOpinion) next() *testOpinion {
	o.sup = append(o.sup, o.pending.sup...)
	o.ag = append(o.ag, o.pending.ag...)
	o.abs = append(o.abs, o.pending.abs...)

	lvote := &layerVote{lid: o.lid, vote: against}
	if len(o.pending.abs) > 0 {
		lvote.vote = neutral
	} else if len(o.pending.sup) > 0 {
		for _, header := range o.pending.sup {
			lvote.supported = append(lvote.supported, newBlockInfo(header))
		}
	}
	o.reference.append(lvote)

	o.pending.sup = nil
	o.pending.ag = nil
	o.pending.abs = nil
	o.lid++
	return o
}

func (o *testOpinion) encodedVotes() types.Votes {
	o = o.next()
	return types.Votes{
		Support: o.sup,
		Against: o.ag,
		Abstain: o.abs,
	}
}

func (o *testOpinion) decodedVotes() votes {
	o = o.next()
	return o.reference
}

func TestStateDecodeVotes(t *testing.T) {
	t.Parallel()
	genesis := types.GetEffectiveGenesis()
	for _, tc := range []struct {
		desc     string
		votes    []*testOpinion // chain of opinions
		expected *testOpinion   // expected final opinion
		err      string
	}{
		{
			"sanity",
			[]*testOpinion{
				newTestOpinion(genesis).support("a", 100),
			},
			newTestOpinion(genesis).support("a", 100),
			"",
		},
		{
			"with abstain",
			[]*testOpinion{
				newTestOpinion(genesis).support("a", 100),
				newTestOpinion(genesis.Add(1)).abstain(),
			},
			newTestOpinion(genesis).support("a", 100).next().abstain(),
			"",
		},
		{
			"cancel support",
			[]*testOpinion{
				newTestOpinion(genesis).support("a", 100),
				newTestOpinion(genesis).against("a", 100),
			},
			newTestOpinion(genesis).next(),
			"",
		},
		{
			"overwrite previos supported",
			[]*testOpinion{
				newTestOpinion(genesis).support("a", 100),
				newTestOpinion(genesis).support("a", 101).
					next().support("b", 100),
			},
			newTestOpinion(genesis).support("a", 101).
				next().support("b", 100),
			"",
		},
		{
			"overwrite previos supported",
			[]*testOpinion{
				newTestOpinion(genesis).support("a", 100),
				newTestOpinion(genesis).support("a", 101).
					next().support("b", 100),
			},
			newTestOpinion(genesis).support("a", 101).
				next().support("b", 100),
			"",
		},
		{
			"wrong last target",
			[]*testOpinion{
				newTestOpinion(genesis).support("a", 100),
				newTestOpinion(genesis).support("a", 101),
				newTestOpinion(genesis).against("a", 100),
			},
			nil,
			"wrong target",
		},
		{
			"conflicting votes",
			[]*testOpinion{
				newTestOpinion(genesis).against("a", 100).against("a", 200),
			},
			nil,
			"conflicting",
		},
		{
			"vote outside window",
			[]*testOpinion{
				newTestOpinion(genesis.Sub(2)).against("a", 100),
			},
			nil,
			"outside the window",
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			base := &ballotInfo{layer: genesis}
			var err error
			for _, opinion := range tc.votes {
				lid := base.layer.Add(1)
				encoded := opinion.encodedVotes()
				var rst votes
				rst, _, err = decodeVotes(genesis.Sub(1), lid, base, encoded)
				if err != nil {
					break
				}
				base = &ballotInfo{layer: lid, votes: rst}
			}
			if len(tc.err) > 0 {
				require.ErrorContains(t, err, tc.err)
			} else {
				require.NoError(t, err)
				ref := tc.expected.decodedVotes()
				require.Equal(t, ref.opinion(), base.opinion())
			}
		})
	}
}
