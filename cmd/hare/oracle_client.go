package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"io"
	"math/big"
	"net/http"
	"sync"
)

const register = "register"
const unregister = "unregister"
const validate = "validatemap"

const defaultOracleServerAddress = "http://localhost:3030"

// serverAddress is the oracle server we're using
var serverAddress = defaultOracleServerAddress

func setServerAddress(addr string) {
	serverAddress = addr
}

type requester interface {
	Get(api, data string) []byte
}

type httpRequester struct {
	url string
	c   *http.Client
}

func newHTTPRequester(url string) *httpRequester {
	return &httpRequester{url, &http.Client{}}
}

func (hr *httpRequester) Get(api, data string) []byte {
	var jsonStr = []byte(data)
	log.Debug("Sending oracle request : %s ", jsonStr)
	req, err := http.NewRequest("POST", hr.url+"/"+api, bytes.NewBuffer(jsonStr))
	if err != nil {
		log.Panic("httpRequester panicked: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := hr.c.Do(req)

	if err != nil {
		log.Panic("httpRequester panicked: %v", err)
	}

	buf := bytes.NewBuffer([]byte{})
	_, err = io.Copy(buf, resp.Body)

	if err != nil {
		log.Panic("httpRequester panicked: %v", err)
	}

	resp.Body.Close()
	return buf.Bytes()
}

// oracleClient is a temporary replacement fot the real oracle. its gets accurate results from a server.
type oracleClient struct {
	world  uint64
	client requester

	eMtx           sync.Mutex
	instMtx        map[uint32]*sync.Mutex
	eligibilityMap map[uint32]map[string]struct{}
}

// newOracleClient creates a new client to query the oracle. It generates a random worldID.
func newOracleClient() *oracleClient {
	b, err := crypto.GetRandomBytes(4)
	if err != nil {
		log.Panic("newOracleClient panicked: %v", err)
	}
	world := big.NewInt(0).SetBytes(b).Uint64()
	return newClientWithWorldID(world)
}

// newClientWithWorldID creates a new client with a specific worldid
func newClientWithWorldID(world uint64) *oracleClient {
	c := newHTTPRequester(serverAddress)
	instMtx := make(map[uint32]*sync.Mutex)
	eligibilityMap := make(map[uint32]map[string]struct{})
	return &oracleClient{world: world, client: c, eligibilityMap: eligibilityMap, instMtx: instMtx}
}

func registerQuery(world uint64, id string, honest bool) string {
	return fmt.Sprintf(`{ "World": %d, "ID": "%v", "Honest": %t }`, world, id, honest)
}

func validateQuery(world uint64, instid uint32, committeeSize int) string {
	return fmt.Sprintf(`{ "World": %d, "InstanceID": %d, "CommitteeSize": %d}`, world, instid, committeeSize)
}

// Register asks the oracle server to add this node to the active set
func (oc *oracleClient) Register(honest bool, id string) {
	oc.client.Get(register, registerQuery(oc.world, id, honest))
}

// Unregister asks the oracle server to de-list this node from the active set
func (oc *oracleClient) Unregister(honest bool, id string) {
	oc.client.Get(unregister, registerQuery(oc.world, id, honest))
}

type validList struct {
	IDs []string `json:"IDs"`
}

func hashInstanceAndK(instanceID types.LayerID, K int32) uint32 {
	kInBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(kInBytes, uint32(K))
	h := newHasherU32()
	val := h.Hash(instanceID.Bytes(), kInBytes)
	return val
}

// Eligible checks whether a given ID is in the eligible list or not. it fetches the list once and gives answers locally after that.
func (oc *oracleClient) Eligible(layer types.LayerID, round int32, committeeSize int, id types.NodeID, sig []byte) (bool, error) {
	instID := hashInstanceAndK(layer, round)
	// make special instance ID
	oc.eMtx.Lock()
	if r, ok := oc.eligibilityMap[instID]; ok {
		_, valid := r[id.Key]
		oc.eMtx.Unlock()
		return valid, nil
	}

	req := validateQuery(oc.world, instID, committeeSize)

	resp := oc.client.Get(validate, req)

	res := &validList{}
	err := json.Unmarshal(resp, res)
	if err != nil {
		panic(err)
	}

	elgMap := make(map[string]struct{})

	for _, v := range res.IDs {
		elgMap[v] = struct{}{}
	}

	_, valid := elgMap[id.Key]

	oc.eligibilityMap[instID] = elgMap

	oc.eMtx.Unlock()
	return valid, nil
}
