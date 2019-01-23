package oracle

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/davecgh/go-spew/spew"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"io"
	"math/big"
	"net/http"
	"sync"
)

const OracleServerAddress = "http://localhost:3030" // todo:configure


type oracleClientDoer interface {
	Do(r *http.Request) (*http.Response, error)
}

// OracleClient is a temporary replacement fot the real oracle. its gets accurate results from a server.
type OracleClient struct {
	world uint64
	client oracleClientDoer

	eMtx sync.Mutex
	instMtx map[int64]*sync.Mutex
	eligibilityMap map[int64]map[string]struct{}
}

// NewOracleClient creates a new client to query the oracle. it generates a random worldid
func NewOracleClient() *OracleClient {
	b, err := crypto.GetRandomBytes(8)
	if err != nil {
		panic(err)
	}
	world := big.NewInt(0).SetBytes(b).Uint64()
	return NewOracleClientWithWorldID(world)
}

// NewOracleClientWithWorldID creates a new client with a specific worldid
func NewOracleClientWithWorldID(world uint64) *OracleClient {
	c := &http.Client{}
	instMtx := make(map[int64]*sync.Mutex)
	eligibilityMap := make(map[int64]map[string]struct{})
	return &OracleClient{world:world, client:c, eligibilityMap: eligibilityMap, instMtx: instMtx}
}

// World returns the world this oracle works in
func (oc *OracleClient) World() uint64 {
	return oc.world
}

func (oc *OracleClient) makeRequest(api string, data string) (*http.Response, error) {
	var jsonStr = []byte(data)
	spew.Println(string(jsonStr))
	req, err := http.NewRequest("POST", OracleServerAddress + "/" + api, bytes.NewBuffer(jsonStr))
	if err != nil {
		panic(err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := oc.client.Do(req)

	if err != nil {
		return nil, err
	}
	return resp, nil
}

// Register asks the oracle server to add this node to the active set
func (oc *OracleClient) Register(stringer fmt.Stringer) {
	reg := fmt.Sprintf(`{ "World": %d, "ID": "%v"}`, oc.world, stringer.String())
	_, err := oc.makeRequest("register", reg)
	if err != nil {
		panic(err)
	}
}

// Unregister asks the oracle server to de-list this node from the active set
func (oc *OracleClient) Unregister(stringer fmt.Stringer) {
	unreg := fmt.Sprintf(`{ "World": %d, "ID": "%v"}`, oc.world,stringer.String())
	_, err := oc.makeRequest("unregister", unreg)
	if err != nil {
		panic(err)
	}
}

type validRes struct {
	Valid bool `json:"valid"`
}

type validList struct {
	IDs []string `json:"IDs"`
}

// NOTE: this is old code, the new Validate fetches the whole map at once instead of requesting for each ID/
// Validate if a proof is valid for a given committee size
func (oc *OracleClient) ValidateSingle(instanceID []byte, K int, committeeSize int, proof []byte, pubKey fmt.Stringer) bool {

	// make special instance ID
	h := newHasherU32()
	val := int64(h.Hash(append(instanceID, byte(K))))

	req := fmt.Sprintf(`{ "World": %d, "InstanceID": %d, "CommitteeSize": %d, "ID": "%v"}`, oc.world, val, committeeSize, pubKey.String())
	resp, err := oc.makeRequest("validate", req)
	if err != nil {
		panic(err)
	}

	buf := bytes.NewBuffer([]byte{})
	_, err = io.Copy(buf, resp.Body)

	if err != nil {
		panic(err)
	}

	res := &validRes{}
	err = json.Unmarshal(buf.Bytes(), res)
	if err != nil {
		panic(err)
	}

	return res.Valid
}


// Validate checks whether a given ID is in the eligible list or not. it fetches the list once and gives answers locally after that.
func (oc *OracleClient) Validate(instanceID []byte, K int, committeeSize int, pubKey fmt.Stringer) bool {

	// make special instance ID
	h := newHasherU32()
	val := int64(h.Hash(append(instanceID, byte(K))))

	oc.eMtx.Lock()
	_, mok := oc.instMtx[val]
	if !mok {
		oc.instMtx[val] = &sync.Mutex{}
	}
	oc.instMtx[val].Lock()
	if r, ok := oc.eligibilityMap[int64(val)]; ok {
			oc.eMtx.Unlock()
			_, valid := r[pubKey.String()]
			oc.instMtx[val].Unlock()
			return valid
		}

	oc.eMtx.Unlock()

	req := fmt.Sprintf(`{ "World": %d, "InstanceID": %d, "CommitteeSize": %d, "ID": "%v"}`, oc.world, val, committeeSize, pubKey.String())
	resp, err := oc.makeRequest("validatemap", req)
	if err != nil {
		panic(err)
	}

	buf := bytes.NewBuffer([]byte{})
	_, err = io.Copy(buf, resp.Body)

	if err != nil {
		panic(err)
	}

	res := &validList{}
	err = json.Unmarshal(buf.Bytes(), res)
	if err != nil {
		panic(err)
	}

	elgmap := make(map[string]struct{})

	for _, v := range res.IDs {
		elgmap[v] = struct{}{}
	}

	_, valid := elgmap[pubKey.String()]

	oc.eligibilityMap[val] = elgmap
	oc.instMtx[val].Unlock()

	return valid
}
