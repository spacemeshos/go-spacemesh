package oracle

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/types"
	"io"
	"math/big"
	"net/http"
	"sync"
)

const Register = "register"
const Unregister = "unregister"
const ValidateSingle = "validate"
const Validate = "validatemap"

const DefaultOracleServerAddress = "http://localhost:3030"

// ServerAddress is the oracle server we're using
var ServerAddress = DefaultOracleServerAddress

func SetServerAddress(addr string) {
	ServerAddress = addr
}

type Requester interface {
	Get(api, data string) []byte
}

type HTTPRequester struct {
	url string
	c   *http.Client
}

func NewHTTPRequester(url string) *HTTPRequester {
	return &HTTPRequester{url, &http.Client{}}
}

func (hr *HTTPRequester) Get(api, data string) []byte {
	var jsonStr = []byte(data)
	log.Debug("Sending oracle request : %s ", jsonStr)
	req, err := http.NewRequest("POST", hr.url+"/"+api, bytes.NewBuffer(jsonStr))
	if err != nil {
		log.Panic("HTTPRequester panicked: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := hr.c.Do(req)

	if err != nil {
		log.Panic("HTTPRequester panicked: %v", err)
	}

	buf := bytes.NewBuffer([]byte{})
	_, err = io.Copy(buf, resp.Body)

	if err != nil {
		log.Panic("HTTPRequester panicked: %v", err)
	}

	resp.Body.Close()
	return buf.Bytes()
}

// OracleClient is a temporary replacement fot the real oracle. its gets accurate results from a server.
type OracleClient struct {
	world  uint64
	client Requester

	eMtx           sync.Mutex
	instMtx        map[uint32]*sync.Mutex
	eligibilityMap map[uint32]map[string]struct{}
}

// NewOracleClient creates a new client to query the oracle. it generates a random worldid
func NewOracleClient() *OracleClient {
	b, err := crypto.GetRandomBytes(4)
	if err != nil {
		log.Panic("NewOracleClient panicked: %v", err)
	}
	world := big.NewInt(0).SetBytes(b).Uint64()
	return NewOracleClientWithWorldID(world)
}

// NewOracleClientWithWorldID creates a new client with a specific worldid
func NewOracleClientWithWorldID(world uint64) *OracleClient {
	c := NewHTTPRequester(ServerAddress)
	instMtx := make(map[uint32]*sync.Mutex)
	eligibilityMap := make(map[uint32]map[string]struct{})
	return &OracleClient{world: world, client: c, eligibilityMap: eligibilityMap, instMtx: instMtx}
}

// World returns the world this oracle works in
func (oc *OracleClient) World() uint64 {
	return oc.world
}

func registerQuery(world uint64, id string, honest bool) string {
	return fmt.Sprintf(`{ "World": %d, "ID": "%v", "Honest": %t }`, world, id, honest)
}

func validateQuery(world uint64, instid uint32, committeeSize int) string {
	return fmt.Sprintf(`{ "World": %d, "InstanceID": %d, "CommitteeSize": %d}`, world, instid, committeeSize)
}

// Register asks the oracle server to add this node to the active set
func (oc *OracleClient) Register(honest bool, id string) {
	oc.client.Get(Register, registerQuery(oc.world, id, honest))
}

// Unregister asks the oracle server to de-list this node from the active set
func (oc *OracleClient) Unregister(honest bool, id string) {
	oc.client.Get(Unregister, registerQuery(oc.world, id, honest))
}

type validRes struct {
	Valid bool `json:"valid"`
}

type validList struct {
	IDs []string `json:"IDs"`
}

// NOTE: this is old code, the new Validate fetches the whole map at once instead of requesting for each ID
func (oc *OracleClient) ValidateSingle(instanceID []byte, K int, committeeSize int, proof []byte, pubKey string) bool {

	// make special instance ID
	h := newHasherU32()
	val := int64(h.Hash(append(instanceID, byte(K))))

	req := fmt.Sprintf(`{ "World": %d, "InstanceID": %d, "CommitteeSize": %d, "ID": "%v"}`, oc.world, val, committeeSize, pubKey)
	resp := oc.client.Get(ValidateSingle, req)

	res := &validRes{}
	err := json.Unmarshal(resp, res)
	if err != nil {
		log.Panic("ValidateSingle panicked: %v", err)
	}

	return res.Valid
}

func hashInstanceAndK(instanceID types.LayerID, K int32) uint32 {
	kInBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(kInBytes, uint32(K))
	h := newHasherU32()
	val := h.Hash(instanceID.ToBytes(), kInBytes)
	return val
}

// Eligible checks whether a given ID is in the eligible list or not. it fetches the list once and gives answers locally after that.
func (oc *OracleClient) Eligible(layer types.LayerID, round int32, committeeSize int, id types.NodeId, sig []byte) (bool, error) {
	instId := hashInstanceAndK(layer, round)
	// make special instance ID
	oc.eMtx.Lock()
	if r, ok := oc.eligibilityMap[instId]; ok {
		_, valid := r[id.Key]
		oc.eMtx.Unlock()
		return valid, nil
	}

	req := validateQuery(oc.world, instId, committeeSize)

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

	_, valid := elgmap[id.Key]

	oc.eligibilityMap[instId] = elgmap

	oc.eMtx.Unlock()
	return valid, nil
}

func (o *OracleClient) Proof(id types.NodeId, layer types.LayerID, round int32) ([]byte, error) {
	return []byte{}, nil
}
func (oc *OracleClient) IsIdentityActive(s string, id types.LayerID) (bool, error) {
	return true, nil
}
