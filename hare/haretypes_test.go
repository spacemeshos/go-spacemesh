package hare

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestSet_Add(t *testing.T) {
	s := NewEmptySet(lowDefaultSize)
	s.Add(value1)
	assert.Equal(t, 1, s.Size())
	s.Add(value2)
	assert.Equal(t, 2, s.Size())
	s.Add(value3)
	assert.Equal(t, 3, s.Size())
	s.Add(value3)
	assert.Equal(t, 3, s.Size())
}

func TestSet_Remove(t *testing.T) {
	s := NewSetFromValues(value1, value2)
	s.Remove(value1)
	assert.Equal(t, 1, s.Size())
	s.Remove(value2)
	assert.Equal(t, 0, s.Size())
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

func TestSet_Complement(t *testing.T) {
	u := NewSetFromValues(value1, value2, value3, value4)
	s := NewSetFromValues(value1, value2, value3)
	exp := NewSetFromValues(value4)
	assert.True(t, exp.Equals(s.Complement(u)))
}

func TestSet_Intersection(t *testing.T) {
	g := NewSetFromValues(value1, value2, value3, value4)
	s := NewSetFromValues(value1, value2, value3)
	assert.True(t, s.Equals(s.Intersection(g)))
}

func TestSet_Contains(t *testing.T) {
	s := NewSmallEmptySet()
	assert.False(t, s.Contains(value1))
	s.Add(value1)
	assert.True(t, s.Contains(value1))
}

func TestSet_IsSubSetOf(t *testing.T) {
	g := NewSetFromValues(value1, value2, value3, value4)
	s := NewSetFromValues(value1, value2, value3)
	assert.True(t, s.IsSubSetOf(g))
}

func TestSet_Size(t *testing.T) {
	s := NewSetFromValues(value1, value2, value3)
	assert.Equal(t, 3, s.Size())
}

func TestSet_Union(t *testing.T) {
	g := NewSetFromValues(value1, value2, value3, value4)
	s := NewSetFromValues(value1, value5)
	exp := NewSetFromValues(value1, value2, value3, value4, value5)
	assert.True(t, exp.Equals(s.Union(g)))
}