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

const Register = "register"
const Unregister = "unregister"
const ValidateSingle = "validate"
const Validate = "validatemap"
const OracleServerAddress = "http://localhost:3030" // todo:configure


type Requester interface {
	Get(api, data string) []byte
}

type HTTPRequester struct {
	url string
	c *http.Client
}

func NewHTTPRequester(url string) *HTTPRequester {
	return &HTTPRequester{url, &http.Client{}}
}

func (hr *HTTPRequester) Get(api, data string) []byte {
	var jsonStr = []byte(data)
	spew.Println(string(jsonStr))
	req, err := http.NewRequest("POST", hr.url + "/" + api, bytes.NewBuffer(jsonStr))
	if err != nil {
		panic(err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := hr.c.Do(req)

	if err != nil {
		panic(err)
	}

	buf := bytes.NewBuffer([]byte{})
	_, err = io.Copy(buf, resp.Body)

	if err != nil {
		panic(err)
	}

	resp.Body.Close()
	return buf.Bytes()
}

// OracleClient is a temporary replacement fot the real oracle. its gets accurate results from a server.
type OracleClient struct {
	world uint64
	client Requester

	eMtx sync.Mutex
	instMtx map[uint32]*sync.Mutex
	eligibilityMap map[uint32]map[string]struct{}
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
	c := NewHTTPRequester(OracleServerAddress)
	instMtx := make(map[uint32]*sync.Mutex)
	eligibilityMap := make(map[uint32]map[string]struct{})
	return &OracleClient{world:world, client:c, eligibilityMap: eligibilityMap, instMtx: instMtx}
}

// World returns the world this oracle works in
func (oc *OracleClient) World() uint64 {
	return oc.world
}

func registerQuery(world uint64, stringer fmt.Stringer) string {
	return fmt.Sprintf(`{ "World": %d, "ID": "%v"}`, world, stringer.String())
}

func validateQuery(world uint64, instid uint32, committeeSize int) string {
	return fmt.Sprintf(`{ "World": %d, "InstanceID": %d, "CommitteeSize": %d}`, world, instid, committeeSize)
}

// Register asks the oracle server to add this node to the active set
func (oc *OracleClient) Register(stringer fmt.Stringer) {
	reg := fmt.Sprintf(`{ "World": %d, "ID": "%v"}`, oc.world, stringer.String())
	oc.client.Get(Register, reg)
}

// Unregister asks the oracle server to de-list this node from the active set
func (oc *OracleClient) Unregister(stringer fmt.Stringer) {
	unreg := fmt.Sprintf(`{ "World": %d, "ID": "%v"}`, oc.world,stringer.String())
	oc.client.Get(Unregister, unreg)
}

type validRes struct {
	Valid bool `json:"valid"`
}

type validList struct {
	IDs []string `json:"IDs"`
}

// NOTE: this is old code, the new Validate fetches the whole map at once instead of requesting for each ID/
  func (oc *OracleClient) ValidateSingle(instanceID []byte, K int, committeeSize int, proof []byte, pubKey fmt.Stringer) bool {

	// make special instance ID
	h := newHasherU32()
	val := int64(h.Hash(append(instanceID, byte(K))))

	req := fmt.Sprintf(`{ "World": %d, "InstanceID": %d, "CommitteeSize": %d, "ID": "%v"}`, oc.world, val, committeeSize, pubKey.String())
	resp := oc.client.Get(ValidateSingle, req)

	res := &validRes{}
	err := json.Unmarshal(resp, res)
	if err != nil {
		panic(err)
	}

	return res.Valid
}

func hashInstanceAndK(instanceID []byte, K int) uint32 {
	h := newHasherU32()
	val := h.Hash(append(instanceID, byte(K)))
	return val
}


// Validate checks whether a given ID is in the eligible list or not. it fetches the list once and gives answers locally after that.
func (oc *OracleClient) Validate(instanceID []byte, K int, committeeSize int, pubKey fmt.Stringer) bool {

	// make special instance ID
	val := hashInstanceAndK(instanceID, K)

	oc.eMtx.Lock()
	_, mok := oc.instMtx[val]
	if !mok {
		oc.instMtx[val] = &sync.Mutex{}
	}
	oc.instMtx[val].Lock()
	if r, ok := oc.eligibilityMap[val]; ok {
			oc.eMtx.Unlock()
			_, valid := r[pubKey.String()]
			oc.instMtx[val].Unlock()
			return valid
		}

	oc.eMtx.Unlock()

	req := validateQuery(oc.world, val,committeeSize)

	resp := oc.client.Get(Validate, req)

	res := &validList{}
	err := json.Unmarshal(resp, res)
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
