package eligibility

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/rand"
)

const (
	numOfClients = 100
	strLen       = 128
	letterBytes  = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
)

const (
	letterIdxBits = 6                    // 6 bits to represent a letter index
	letterIdxMask = 1<<letterIdxBits - 1 // All 1-bits, as many as letterIdxBits
)

func genNode() types.NodeID {
	b := types.NodeID{}
	rand.Read(b[:])
	return b
}

func TestFixedRolacle_Eligible(t *testing.T) {
	oracle := New(logtest.New(t))
	for i := 0; i < numOfClients-1; i++ {
		oracle.Register(true, genNode())
	}
	v := genNode()
	oracle.Register(true, v)

	res, _ := oracle.eligible(context.TODO(), types.NewLayerID(1), 1, 10, v, nil)
	res2, _ := oracle.eligible(context.TODO(), types.NewLayerID(1), 1, 10, v, nil)
	assert.True(t, res == res2)
}

func TestFixedRolacle_Eligible2(t *testing.T) {
	pubs := make([]types.NodeID, 0, numOfClients)
	oracle := New(logtest.New(t))
	for i := 0; i < numOfClients; i++ {
		s := genNode()
		pubs = append(pubs, s)
		oracle.Register(true, s)
	}

	count := 0
	for _, p := range pubs {
		res, _ := oracle.eligible(context.TODO(), types.NewLayerID(1), 1, 10, p, nil)
		if res {
			count++
		}
	}

	assert.Equal(t, 10, count)

	count = 0
	for _, p := range pubs {
		res, _ := oracle.eligible(context.TODO(), types.NewLayerID(1), 1, 20, p, nil)
		if res {
			count++
		}
	}

	assert.Equal(t, 10, count)
}

func TestFixedRolacle_Range(t *testing.T) {
	oracle := New(logtest.New(t))
	pubs := make([]types.NodeID, 0, numOfClients)
	for i := 0; i < numOfClients; i++ {
		s := genNode()
		pubs = append(pubs, s)
		oracle.Register(true, s)
	}

	count := 0
	for _, p := range pubs {
		res, _ := oracle.eligible(context.TODO(), types.NewLayerID(1), 1, numOfClients, p, nil)
		if res {
			count++
		}
	}

	// check all eligible
	assert.Equal(t, numOfClients, count)

	count = 0
	for _, p := range pubs {
		res, _ := oracle.eligible(context.TODO(), types.NewLayerID(2), 1, 0, p, nil)
		if res {
			count++
		}
	}

	// check all not eligible
	assert.Equal(t, 0, count)
}

func TestFixedRolacle_Eligible3(t *testing.T) {
	oracle := New(logtest.New(t))
	for i := 0; i < numOfClients/3; i++ {
		s := genNode()
		oracle.Register(true, s)
	}

	for i := 0; i < 2*numOfClients/3; i++ {
		s := genNode()
		oracle.Register(false, s)
	}

	exp := numOfClients / 2
	ok, err := oracle.eligible(context.TODO(), types.NewLayerID(1), 1, exp, types.NodeID{1}, nil)
	require.NoError(t, err)
	require.False(t, ok)

	hc := 0
	for k := range oracle.honest {
		res, _ := oracle.eligible(context.TODO(), types.NewLayerID(1), 1, exp, k, nil)
		if res {
			hc++
		}
	}

	dc := 0
	for k := range oracle.faulty {
		res, _ := oracle.eligible(context.TODO(), types.NewLayerID(1), 1, exp, k, nil)
		if res {
			dc++
		}
	}

	assert.Equal(t, exp/2+1, hc)
	assert.Equal(t, exp/2-1, dc)
}

func TestGenerateElibility(t *testing.T) {
	oracle := New(logtest.New(t))
	ids := []types.NodeID{}
	for i := 0; i < 30; i++ {
		s := genNode()
		ids = append(ids, s)
		oracle.Register(true, s)
	}

	m := oracle.generateEligibility(context.TODO(), len(oracle.honest))

	for _, s := range ids {
		_, ok := m[s]
		require.True(t, ok)
	}
}

func TestFixedRolacle_Eligible4(t *testing.T) {
	oracle := New(logtest.New(t))
	var ids []types.NodeID
	for i := 0; i < 33; i++ {
		s := genNode()
		ids = append(ids, s)
		oracle.Register(true, s)
	}

	// when requesting a bigger committee size everyone should be eligible

	for _, s := range ids {
		res, _ := oracle.eligible(context.TODO(), types.LayerID{}, 1, numOfClients, s, nil)
		assert.True(t, res)
	}
}

func TestFixedRolacle_Export(t *testing.T) {
	oracle := New(logtest.New(t))
	var ids []types.NodeID
	for i := 0; i < 35; i++ {
		s := genNode()
		ids = append(ids, s)
		oracle.Register(true, s)
	}

	// when requesting a bigger committee size everyone should be eligible

	m := oracle.Export(types.RandomHash(), numOfClients)

	for _, s := range ids {
		_, ok := m[s]
		assert.True(t, ok)
	}
}
