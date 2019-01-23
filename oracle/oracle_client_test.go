package oracle

import (
	"github.com/btcsuite/btcutil/base58"
	"github.com/stretchr/testify/assert"
	"math/rand"
	"net/http"
	"sync"
	"testing"
)

type stringer string

func (s stringer) String() string {
	return string(s)
}

func generateID() stringer {
	rnd := make([]byte, 32)
	rand.Read(rnd)
	//that's hilarius
	return stringer(base58.Encode(rnd))
}
//
func Test_OracleClientValidate(t *testing.T) {
	size := 100
	committee := 30

	oc := NewOracleClient()

	pks := make([]stringer, size)

	for i:=0;i<size;i++ {
		pk := generateID()
		pks[i] = pk
		oc.Register(pk)
	}


	incommitte := 0

	for i := 0; i < size; i++ {
		if oc.Validate([]byte{010}, 2, committee,  pks[i]) {
		incommitte++
		}
	}

	assert.Equal(t, incommitte, committee)

	for i := 0; i < size; i++ {
		oc.Unregister(pks[i])
	}
}


type mockClientDoer struct {
	requester oracleClientDoer // Real requester
	mtx sync.Mutex
	count bool
	reqCounter int
}


func (mcd *mockClientDoer) Do(r *http.Request) (*http.Response, error) {
	mcd.mtx.Lock()
	if mcd.count {
		mcd.reqCounter++
	}
	mcd.mtx.Unlock()

	//todo :impl without a real request.

	return mcd.requester.Do(r)
}

func (mcd *mockClientDoer) setCounting(b bool) {
	mcd.mtx.Lock()
	mcd.count =b
	mcd.mtx.Unlock()
}


func Test_Concurrency(t *testing.T) {
	size := 100
	committee := 8

	oc := NewOracleClient()

	pks := make([]stringer, size)

	for i:=0;i<size;i++ {
		pk := generateID()
		pks[i] = pk
		oc.Register(pk)
	}

	inst := []byte{0, 1}

	incommitte := 0

	mc := &mockClientDoer{requester:oc.client}

	oc.client = mc
	mc.setCounting(true)
	for i := 0; i < size; i++ {
		if oc.Validate(inst, 1, committee, pks[i]) {
			incommitte++
		}
	}
	assert.Equal(t, incommitte, committee)
	mc.setCounting(false)

	for i := 0; i < size; i++ {
		oc.Unregister(pks[i])
	}

	assert.Equal(t, mc.reqCounter, 1)
}