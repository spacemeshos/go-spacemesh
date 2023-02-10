package hare

import (
	"bytes"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func TestSet_Add(t *testing.T) {
	s := NewEmptySet(lowDefaultSize)
	s.Add(types.ProposalID{1})
	require.Equal(t, 1, s.Size())
	s.Add(types.ProposalID{2})
	require.Equal(t, 2, s.Size())
	s.Add(types.ProposalID{3})
	require.Equal(t, 3, s.Size())
	s.Add(types.ProposalID{3})
	require.Equal(t, 3, s.Size())
}

func TestSet_Remove(t *testing.T) {
	s := NewSetFromValues(types.ProposalID{1}, types.ProposalID{2})
	s.Remove(types.ProposalID{1})
	require.Equal(t, 1, s.Size())
	s.Remove(types.ProposalID{2})
	require.Equal(t, 0, s.Size())
}

func TestSet_Equals(t *testing.T) {
	s1 := NewEmptySet(lowDefaultSize)
	s1.Add(types.ProposalID{1})
	s1.Add(types.ProposalID{2})

	s2 := NewEmptySet(lowDefaultSize)
	s2.Add(types.ProposalID{2})
	require.False(t, s1.Equals(s2))
	require.False(t, s2.Equals(s1))

	s2.Add(types.ProposalID{1})
	require.True(t, s1.Equals(s2))
	require.True(t, s2.Equals(s1))
}

func TestSet_Id(t *testing.T) {
	s1 := NewEmptySet(lowDefaultSize)
	s1.Add(types.ProposalID{1})
	s1.Add(types.ProposalID{2})

	s2 := NewEmptySet(lowDefaultSize)
	s2.Add(types.ProposalID{1})
	require.NotEqual(t, s1.ID(), s2.ID())
	s2.Add(types.ProposalID{2})
	require.Equal(t, s1.ID(), s2.ID())

	s3 := NewEmptySet(lowDefaultSize)
	s3.Add(types.ProposalID{2})
	s3.Add(types.ProposalID{1})
	s3.Add(types.ProposalID{3})

	s1.Add(types.ProposalID{3})
	require.Equal(t, s1.ID(), s3.ID())
}

func TestSet_Complement(t *testing.T) {
	u := NewSetFromValues(types.ProposalID{1}, types.ProposalID{2}, types.ProposalID{3}, types.ProposalID{4})
	s := NewSetFromValues(types.ProposalID{1}, types.ProposalID{2}, types.ProposalID{3})
	exp := NewSetFromValues(types.ProposalID{4})
	require.True(t, exp.Equals(s.Complement(u)))
}

func TestSet_Intersection(t *testing.T) {
	g := NewSetFromValues(types.ProposalID{1}, types.ProposalID{2}, types.ProposalID{3}, types.ProposalID{4})
	s := NewSetFromValues(types.ProposalID{1}, types.ProposalID{2}, types.ProposalID{3})
	require.True(t, s.Equals(s.Intersection(g)))
}

func TestSet_Contains(t *testing.T) {
	s := NewDefaultEmptySet()
	require.False(t, s.Contains(types.ProposalID{1}))
	s.Add(types.ProposalID{1})
	require.True(t, s.Contains(types.ProposalID{1}))
}

func TestSet_IsSubSetOf(t *testing.T) {
	g := NewSetFromValues(types.ProposalID{1}, types.ProposalID{2}, types.ProposalID{3}, types.ProposalID{4})
	s := NewSetFromValues(types.ProposalID{1}, types.ProposalID{2}, types.ProposalID{3})
	require.True(t, s.IsSubSetOf(g))
}

func TestSet_Size(t *testing.T) {
	s := NewSetFromValues(types.ProposalID{1}, types.ProposalID{2}, types.ProposalID{3})
	require.Equal(t, 3, s.Size())
}

func TestSet_Union(t *testing.T) {
	g := NewSetFromValues(types.ProposalID{1}, types.ProposalID{2}, types.ProposalID{3}, types.ProposalID{4})
	s := NewSetFromValues(types.ProposalID{1}, types.ProposalID{5})
	exp := NewSetFromValues(types.ProposalID{1}, types.ProposalID{2}, types.ProposalID{3}, types.ProposalID{4}, types.ProposalID{5})
	require.True(t, exp.Equals(s.Union(g)))
}

func TestSet_ToSlice(t *testing.T) {
	arr := []types.ProposalID{{7}, {1}, {5}, {6}, {2}, {3}, {4}}
	s := NewSet(arr)
	res := s.ToSlice()
	sort.Slice(arr, func(i, j int) bool { return bytes.Compare(arr[i].Bytes(), arr[j].Bytes()) == -1 })
	require.Equal(t, arr, res) // check result is sorted, required for order of set in commit msgs
}
