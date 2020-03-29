package metrics

import (
	"github.com/go-kit/kit/metrics"
	"github.com/stretchr/testify/require"
	"testing"
)

type mockCache struct {
	AddFunc func(key, value interface{}) bool
	GetFunc func(key interface{}) (value interface{}, ok bool)
}

func (mc *mockCache) Add(key, value interface{}) bool {
	if mc.AddFunc != nil {
		return mc.AddFunc(key, value)
	}
	return false
}
func (mc *mockCache) Get(key interface{}) (value interface{}, ok bool) {
	if mc.GetFunc != nil {
		return mc.GetFunc(key)
	}
	return nil, false
}

var _ Cache = (*mockCache)(nil)

type mockCounter struct {
	WithFunc func(labelValues ...string) metrics.Counter
	AddFunc  func(delta float64)
}

func (mcnt *mockCounter) Add(delta float64) {
	if mcnt.AddFunc != nil {
		mcnt.AddFunc(delta)
	}
}

func (mcnt *mockCounter) With(labelValues ...string) metrics.Counter {
	if mcnt.WithFunc != nil {
		return mcnt.WithFunc(labelValues...)
	}
	return mcnt
}

type mockGauge struct {
	WithFunc func(labelValues ...string) metrics.Gauge
	AddFunc  func(delta float64)
	SetFunc  func(delta float64)
}

func (mcnt *mockGauge) Add(delta float64) {
	if mcnt.AddFunc != nil {
		mcnt.AddFunc(delta)
	}
}

func (mcnt *mockGauge) Set(delta float64) {
	if mcnt.SetFunc != nil {
		mcnt.SetFunc(delta)
	}
}

func (mcnt *mockGauge) With(labelValues ...string) metrics.Gauge {
	if mcnt.WithFunc != nil {
		return mcnt.WithFunc(labelValues...)
	}
	return mcnt
}

func TestMeteredCache_Add(t *testing.T) {
	mc := &mockCache{
		AddFunc: nil,
		GetFunc: nil,
	}

	mtrd := NewMeteredCache(mc, "testCache", "mytest", "cachefortest", nil)

	added := float64(0)

	mtrd.count = &mockGauge{
		WithFunc: nil,
		AddFunc: func(delta float64) {
			added += delta
		},
		SetFunc: nil,
	}

	b := mtrd.Add("key", "value")
	require.False(t, b)
	require.Equal(t, float64(1), added)

	mc.AddFunc = func(key, value interface{}) bool {
		return true
	}

	b = mtrd.Add("key", "value")
	require.True(t, b)
	require.Equal(t, float64(1), added)
}

func TestMeteredCache_Get(t *testing.T) {
	mc := &mockCache{
		AddFunc: nil,
		GetFunc: nil,
	}

	mtrd := NewMeteredCache(mc, "testCache", "mytest", "cachefortest", nil)

	hits := float64(0)

	mtrd.hits = &mockCounter{
		WithFunc: nil,
		AddFunc: func(delta float64) {
			hits += delta
		},
	}
	miss := float64(0)

	mtrd.miss = &mockCounter{
		WithFunc: nil,
		AddFunc: func(delta float64) {
			miss += delta
		},
	}

	v, ok := mtrd.Get("KEYY")
	require.Nil(t, v)
	require.False(t, ok)
	require.Equal(t, float64(1), miss)

	mc.GetFunc = func(key interface{}) (value interface{}, ok bool) {
		return "value", true
	}

	v, ok = mtrd.Get("KEYY")
	require.Equal(t, "value", v)
	require.True(t, ok)
	require.Equal(t, float64(1), hits)
}
