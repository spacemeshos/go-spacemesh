package hare

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestSet_AddRemove(t *testing.T) {
	s := NewEmptySet()
	s.Add(blockId1)
	assert.True(t, s.Contains(blockId1))
	s.Add(blockId2)
	assert.True(t, s.Contains(blockId2))
	s.Add(blockId3)
	assert.True(t, s.Contains(blockId3))
	s.Remove(blockId1)
	assert.False(t, s.Contains(blockId1))
}

func TestSet_Equals(t *testing.T) {
	s1 := NewEmptySet()
	s1.Add(blockId1)
	s1.Add(blockId2)

	s2 := NewEmptySet()
	s2.Add(blockId2)
	assert.False(t, s1.Equals(s2))
	assert.False(t, s2.Equals(s1))

	s2.Add(blockId1)
	assert.True(t, s1.Equals(s2))
	assert.True(t, s2.Equals(s1))
}

func TestSet_Id(t *testing.T) {
	s1 := NewEmptySet()
	s1.Add(blockId1)
	s1.Add(blockId2)

	s2 := NewEmptySet()
	s2.Add(blockId1)
	assert.NotEqual(t, s1.Id(), s2.Id())
	s2.Add(blockId2)
	assert.Equal(t, s1.Id(), s2.Id())

	s3 := NewEmptySet()
	s3.Add(blockId2)
	s3.Add(blockId1)
	s3.Add(blockId3)

	s1.Add(blockId3)
	assert.Equal(t, s1.Id(), s3.Id())
}
