package hare

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestSet_AddRemove(t *testing.T) {
	s := NewEmptySet(lowDefaultSize)
	s.Add(value1)
	assert.True(t, s.Contains(value1))
	s.Add(value2)
	assert.True(t, s.Contains(value2))
	s.Add(value3)
	assert.True(t, s.Contains(value3))
	s.Remove(value1)
	assert.False(t, s.Contains(value1))
}

func TestSet_Equals(t *testing.T) {
	s1 := NewEmptySet(lowDefaultSize)
	s1.Add(value1)
	s1.Add(value2)

	s2 := NewEmptySet(lowDefaultSize)
	s2.Add(value2)
	assert.False(t, s1.Equals(s2))
	assert.False(t, s2.Equals(s1))

	s2.Add(value1)
	assert.True(t, s1.Equals(s2))
	assert.True(t, s2.Equals(s1))
}

func TestSet_Id(t *testing.T) {
	s1 := NewEmptySet(lowDefaultSize)
	s1.Add(value1)
	s1.Add(value2)

	s2 := NewEmptySet(lowDefaultSize)
	s2.Add(value1)
	assert.NotEqual(t, s1.Id(), s2.Id())
	s2.Add(value2)
	assert.Equal(t, s1.Id(), s2.Id())

	s3 := NewEmptySet(lowDefaultSize)
	s3.Add(value2)
	s3.Add(value1)
	s3.Add(value3)

	s1.Add(value3)
	assert.Equal(t, s1.Id(), s3.Id())
}
