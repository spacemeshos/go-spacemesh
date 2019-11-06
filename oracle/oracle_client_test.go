package oracle

import (
	"fmt"
	"github.com/btcsuite/btcutil/base58"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
)

const TestServerOnline = false

func generateID() string {
	rnd := make([]byte, 32)
	rand.Read(rnd)
	return base58.Encode(rnd)
}

type requestCounter struct {
	client     Requester
	mtx        sync.Mutex
	count      bool
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
	mcd.count = b
	mcd.mtx.Unlock()
}

type mockRequester struct {
	resMutex sync.Mutex
	results  map[string][]byte
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
	mr := &mockRequester{results: make(map[string][]byte)}
	id := generateID()
	mr.SetResult(Register, id, []byte(`{ "message": "ok" }"`))
	counter := &requestCounter{client: mr}
	counter.setCounting(true)
	oc.client = counter
	oc.Register(true, id)
	require.Equal(t, counter.reqCounter, 1)

	mr.SetResult(Validate, validateQuery(oc.world, hashInstanceAndK(0, 0), 2),
		[]byte(fmt.Sprintf(`{ "IDs": [ "%v" ] }`, id)))

	valid, _ := oc.Eligible(0, 0, 2, types.NodeId{Key: id}, nil)

	require.True(t, valid)

	valid, _ = oc.Eligible(0, 0, 2, types.NodeId{Key: generateID()}, nil)

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

	pks := make([]string, size)

	for i := 0; i < size; i++ {
		pk := generateID()
		pks[i] = pk
		oc.Register(true, pk)
	}

	incommitte := 0

	for i := 0; i < size; i++ {
		res, _ := oc.Eligible(0, 0, committee, types.NodeId{Key: pks[i]}, nil)
		if res {
			incommitte++
		}
	}

	assert.Equal(t, incommitte, committee)

	for i := 0; i < size; i++ {
		oc.Unregister(true, pks[i])
	}
}

func Test_Concurrency(t *testing.T) {
	if !TestServerOnline {
		t.Skip()
	}

	size := 1000
	committee := 80

	oc := NewOracleClient()

	pks := make([]string, size)

	for i := 0; i < size; i++ {
		pk := generateID()
		pks[i] = pk
		oc.Register(true, pk)
	}

	incommitte := 0

	mc := &requestCounter{client: oc.client}

	oc.client = mc
	mc.setCounting(true)
	for i := 0; i < size; i++ {
		res, _ := oc.Eligible(0, 0, committee, types.NodeId{Key: pks[i]}, nil)
		if res {
			incommitte++
		}
	}
	assert.Equal(t, incommitte, committee)
	mc.setCounting(false)

	for i := 0; i < size; i++ {
		oc.Unregister(true, pks[i])
	}

	assert.Equal(t, mc.reqCounter, 1)
}

func TestOracle_Eligible2(t *testing.T) {
	o := NewOracleClient()
	mr := &mockRequester{results: make(map[string][]byte)}
	//id := generateID()
	mr.SetResult(Register, "myid", []byte(`{ "message": "ok" }"`))
	o.client = mr
	o.Register(true, "myid")
	mr.SetResult(Validate, validateQuery(o.world, hashInstanceAndK(1, 2), 0),
		[]byte(fmt.Sprintf(`{ "IDs": [ "%v" ] }`, "sheker")))
	res, err := o.Eligible(1, 2, 0, types.NodeId{}, []byte{})
	assert.Nil(t, err)
	assert.False(t, res)
	mr.SetResult(Validate, validateQuery(o.world, hashInstanceAndK(1, 3), 1),
		[]byte(fmt.Sprintf(`{ "IDs": [ "%v" ] }`, "sheker")))
	res, err = o.Eligible(1, 3, 1, types.NodeId{}, []byte{})
	assert.Nil(t, err)
	assert.False(t, res)
}
