package oracle

import (
	"fmt"
	"github.com/btcsuite/btcutil/base58"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"math/rand"
	"sync"
	"testing"
)

const TestServerOnline = false

type stringer string

func (s stringer) String() string {
	return string(s)
}

func generateID() stringer {
	rnd := make([]byte, 32)
	rand.Read(rnd)
	return stringer(base58.Encode(rnd))
}

type requestCounter struct {
	client Requester
	mtx sync.Mutex
	count bool
	reqCounter int
}

func (mcd *requestCounter) Get(api, data string) []byte {
	var res []byte
	mcd.mtx.Lock()
	if mcd.count {
		mcd.reqCounter++
	}
	if mcd.client != nil {
		res = mcd.client.Get(api, data)
	}
	mcd.mtx.Unlock()
	return res
}

func (mcd *requestCounter) setCounting(b bool) {
	mcd.mtx.Lock()
	mcd.count =b
	mcd.mtx.Unlock()
}

type mockRequester struct {
	resMutex sync.Mutex
	results map[string][]byte
}


func (mcd *mockRequester) SetResult(api, data string, res []byte) {
	mcd.results[api+data] = res
}

func (mcd *mockRequester) Get(api, data string) []byte {
	r, ok := mcd.results[api+data]
	if ok {
		return r
	}
	return nil
}


func Test_MockOracleClientValidate(t *testing.T) {
	oc := NewOracleClient()
	mr := &mockRequester{results:make(map[string][]byte)}
	id := generateID()
	mr.SetResult(Register, id.String(), []byte(`{ "message": "ok" }"`))
	counter := &requestCounter{client:mr}
	counter.setCounting(true)
	oc.client = counter
	oc.Register(id)
	require.Equal(t, counter.reqCounter, 1)

	instid := hashInstanceAndK([]byte{0}, 1)

	mr.SetResult(Validate, validateQuery(oc.world, instid, 2),
		[]byte(fmt.Sprintf(`{ "IDs": [ "%v" ] }`, id.String())))

	valid := oc.Validate([]byte{0}, 1, 2, id)

	require.True(t, valid)

	valid = oc.Validate([]byte{0}, 1, 2, generateID())

	require.Equal(t, counter.reqCounter, 2)
	require.False(t, valid)
}

func Test_OracleClientValidate(t *testing.T) {
	if !TestServerOnline {
		t.Skip()
	}
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


func Test_Concurrency(t *testing.T) {
	if !TestServerOnline {
		t.Skip()
	}

	size := 1000
	committee := 80

	oc := NewOracleClient()

	pks := make([]stringer, size)

	for i:=0;i<size;i++ {
		pk := generateID()
		pks[i] = pk
		oc.Register(pk)
	}

	inst := []byte{0, 1}

	incommitte := 0

	mc := &requestCounter{client:oc.client}

	oc.client = mc
	mc.setCounting(true)
	for i := 0; i < size; i++ {
		if oc.Validate(inst, -1, committee, pks[i]) {
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