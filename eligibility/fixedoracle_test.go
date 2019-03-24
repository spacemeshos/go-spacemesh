package eligibility

import (
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

const numOfClients = 100
const strLen = 128
const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
const (
	letterIdxBits = 6                    // 6 bits to represent a letter index
	letterIdxMask = 1<<letterIdxBits - 1 // All 1-bits, as many as letterIdxBits
)

func genStr() string {
	b := make([]byte, strLen)
	for i := 0; i < strLen; {
		if idx := int(rand.Int63() & letterIdxMask); idx < len(letterBytes) {
			b[i] = letterBytes[idx]
			i++
		}
	}
	return string(b)
}

func TestFixedRolacle_Eligible(t *testing.T) {
	oracle := New()
	for i := 0; i < numOfClients-1; i++ {
		oracle.Register(true, genStr())
	}
	v := genStr()
	oracle.Register(true, v)

	res := oracle.Eligible(1, 10, v, nil)
	assert.True(t, res == oracle.Eligible(1, 10, v, nil))
}

func TestFixedRolacle_Eligible2(t *testing.T) {
	pubs := make([]string, 0, numOfClients)
	oracle := New()
	for i := 0; i < numOfClients; i++ {
		s := genStr()
		pubs = append(pubs, s)
		oracle.Register(true, s)
	}

	count := 0
	for _, p := range pubs {
		if oracle.Eligible(1, 10, p, nil) {
			count++
		}
	}

	assert.Equal(t, 10, count)

	count = 0
	for _, p := range pubs {
		if oracle.Eligible(1, 20, p, nil) {
			count++
		}
	}

	assert.Equal(t, 10, count)
}

func TestFixedRolacle_Range(t *testing.T) {
	oracle := New()
	pubs := make([]string, 0, numOfClients)
	for i := 0; i < numOfClients; i++ {
		s := genStr()
		pubs = append(pubs, s)
		oracle.Register(true, s)
	}

	count := 0
	for _, p := range pubs {
		if oracle.Eligible(1, numOfClients, p, nil) {
			count++
		}
	}

	// check all eligible
	assert.Equal(t, numOfClients, count)

	count = 0
	for _, p := range pubs {
		if oracle.Eligible(2, 0, p, nil) {
			count++
		}
	}

	// check all not eligible
	assert.Equal(t, 0, count)
}

func TestFixedRolacle_Eligible3(t *testing.T) {
	oracle := New()
	for i := 0; i < numOfClients/3; i++ {
		s := genStr()
		oracle.Register(true, s)
	}

	for i := 0; i < 2*numOfClients/3; i++ {
		s := genStr()
		oracle.Register(false, s)
	}

	exp := numOfClients / 2
	oracle.Eligible(1, exp, "", nil)

	hc := 0
	for k := range oracle.honest {
		if oracle.Eligible(1, exp, k, nil) {
			hc++
		}
	}

	dc := 0
	for k := range oracle.faulty {
		if oracle.Eligible(1, exp, k, nil) {
			dc++
		}
	}

	assert.Equal(t, exp/2+1, hc)
	assert.Equal(t, exp/2-1, dc)
}

func TestGenerateElibility(t *testing.T) {
	oracle := New()
	ids := []string{}
	for i := 0; i < 30; i++ {
		s := genStr()
		ids = append(ids, s)
		oracle.Register(true, s)
	}

	m := oracle.generateEligibility(len(oracle.honest))

	for _, s := range ids {
		_, ok := m[s]
		require.True(t, ok)
	}
}

func TestFixedRolacle_Eligible4(t *testing.T) {
	oracle := New()
	var ids []string
	for i := 0; i < 33; i++ {
		s := genStr()
		ids = append(ids, s)
		oracle.Register(true, s)
	}

	// when requesting a bigger committee size everyone should be eligible

	for _, s := range ids {
		assert.True(t, oracle.Eligible(0, numOfClients, s, nil))
	}
}

func TestFixedRolacle_Export(t *testing.T) {
	oracle := New()
	var ids []string
	for i := 0; i < 35; i++ {
		s := genStr()
		ids = append(ids, s)
		oracle.Register(true, s)
	}

	// when requesting a bigger committee size everyone should be eligible

	m := oracle.Export(0, numOfClients)

	for _, s := range ids {
		_, ok := m[s]
		assert.True(t, ok)
	}
}
