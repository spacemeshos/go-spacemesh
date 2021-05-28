// Package eligibility defines fixed size oracle used for node testing
package eligibility

import (
	"context"
	"encoding/binary"
	"hash/fnv"
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

// FixedRolacle is an eligibility simulator with pre-determined honest and faulty participants.
type FixedRolacle struct {
	honest map[string]struct{}
	faulty map[string]struct{}
	emaps  map[uint32]map[string]struct{}
	mutex  sync.Mutex
	mapRW  sync.RWMutex
}

// New initializes the oracle with no participants.
func New() *FixedRolacle {
	rolacle := &FixedRolacle{}
	rolacle.honest = make(map[string]struct{})
	rolacle.faulty = make(map[string]struct{})
	rolacle.emaps = make(map[uint32]map[string]struct{})

	return rolacle
}

// IsIdentityActiveOnConsensusView is use to satisfy the API, currently always returns true.
func (fo *FixedRolacle) IsIdentityActiveOnConsensusView(edID string, layer types.LayerID) (bool, error) {
	return true, nil
}

// Export creates a map with the eligible participants for id and committee size.
func (fo *FixedRolacle) Export(id uint32, committeeSize int) map[string]struct{} {
	fo.mapRW.RLock()
	total := len(fo.honest) + len(fo.faulty)
	fo.mapRW.RUnlock()

	// normalize committee size
	size := committeeSize
	if committeeSize > total {
		log.AppLog.With().Warning("committee size bigger than the number of clients",
			log.Int("committee_size", committeeSize),
			log.Int("num_clients", total))
		size = total
	}

	fo.mapRW.Lock()
	// generate if not exist for the requested K
	if _, exist := fo.emaps[id]; !exist {
		fo.emaps[id] = fo.generateEligibility(context.TODO(), size)
	}
	m := fo.emaps[id]
	fo.mapRW.Unlock()

	return m
}

func (fo *FixedRolacle) update(m map[string]struct{}, client string) {
	fo.mutex.Lock()

	if _, exist := m[client]; exist {
		fo.mutex.Unlock()
		return
	}

	m[client] = struct{}{}

	fo.mutex.Unlock()
}

// Register adds a participant to the eligibility map. can be honest or faulty.
func (fo *FixedRolacle) Register(isHonest bool, client string) {
	if isHonest {
		fo.update(fo.honest, client)
	} else {
		fo.update(fo.faulty, client)
	}
}

// Unregister removes a participant from the eligibility map. can be honest or faulty.
// TODO: just remove from both instead of specifying.
func (fo *FixedRolacle) Unregister(isHonest bool, client string) {
	fo.mutex.Lock()
	if isHonest {
		delete(fo.honest, client)
	} else {
		delete(fo.faulty, client)
	}
	fo.mutex.Unlock()
}

func cloneMap(m map[string]struct{}) map[string]struct{} {
	c := make(map[string]struct{}, len(m))
	for k, v := range m {
		c[k] = v
	}

	return c
}

func pickUnique(pickCount int, orig map[string]struct{}, dest map[string]struct{}) {
	i := 0
	for k := range orig { // randomly pass on clients
		if i == pickCount { // pick exactly size
			break
		}

		dest[k] = struct{}{}
		delete(orig, k) // unique pick
		i++
	}
}

func (fo *FixedRolacle) generateEligibility(ctx context.Context, expCom int) map[string]struct{} {
	logger := log.AppLog.WithContext(ctx)
	emap := make(map[string]struct{}, expCom)

	if expCom == 0 {
		return emap
	}

	expHonest := expCom/2 + 1
	if expHonest > len(fo.honest) {
		logger.With().Warning("not enough registered honest participants",
			log.Int("expected", expHonest),
			log.Int("actual", len(fo.honest)))
		expHonest = len(fo.honest)
	}

	hon := cloneMap(fo.honest)
	pickUnique(expHonest, hon, emap)

	expFaulty := expCom - expHonest
	if expFaulty > len(fo.faulty) {
		if len(fo.faulty) > 0 { // not enough
			logger.With().Debug("not enough registered dishonest participants to pick from, picking all faulty",
				log.Int("expected", expFaulty),
				log.Int("actual", len(fo.faulty)))
		} else { // no faulty at all - acceptable
			logger.Debug("no registered dishonest participants to pick from, picking honest instead")
		}
		expFaulty = len(fo.faulty)
	}

	if expFaulty > 0 { // pick faulty if you need
		fau := cloneMap(fo.faulty)
		pickUnique(expFaulty, fau, emap)
	}

	rem := expCom - expHonest - expFaulty
	if rem > 0 { // need to pickUnique the remaining from honest
		pickUnique(rem, hon, emap)
	}

	return emap
}

func hashLayerAndRound(instanceID types.LayerID, round int32) uint32 {
	kInBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(kInBytes, uint32(round))
	h := fnv.New32()
	_, err := h.Write(instanceID.Bytes())
	_, err2 := h.Write(kInBytes)

	if err != nil || err2 != nil {
		log.AppLog.With().Error("errors trying to create a hash",
			log.FieldNamed("err1", log.Err(err)),
			log.FieldNamed("err2", log.Err(err2)))
	}

	return h.Sum32()
}

// Validate is required to conform to the Rolacle interface, but should never be called.
func (fo *FixedRolacle) Validate(context.Context, types.LayerID, int32, int, types.NodeID, []byte, uint16) (bool, error) {
	panic("implement me!")
}

// CalcEligibility returns 1 if the miner is eligible in given layer, and 0 otherwise.
func (fo *FixedRolacle) CalcEligibility(ctx context.Context, layer types.LayerID, round int32, committeeSize int, id types.NodeID, sig []byte) (uint16, error) {
	eligible, err := fo.eligible(ctx, layer, round, committeeSize, id, sig)
	if eligible {
		return 1, nil
	}
	return 0, err
}

// eligible returns whether the specific NodeID is eligible for layer in round and committee size.
func (fo *FixedRolacle) eligible(ctx context.Context, layer types.LayerID, round int32, committeeSize int, id types.NodeID, sig []byte) (bool, error) {
	fo.mapRW.RLock()
	total := len(fo.honest) + len(fo.faulty) // safe since len >= 0
	fo.mapRW.RUnlock()

	// normalize committee size
	size := committeeSize
	if committeeSize > total {
		log.AppLog.WithContext(ctx).With().Warning("committee size bigger than the number of clients",
			log.Int("committee_size", committeeSize),
			log.Int("num_clients", total))
		size = total
	}

	instID := hashLayerAndRound(layer, round)

	fo.mapRW.Lock()
	// generate if not exist for the requested K
	if _, exist := fo.emaps[instID]; !exist {
		fo.emaps[instID] = fo.generateEligibility(ctx, size)
	}
	fo.mapRW.Unlock()
	// get eligibility result
	_, exist := fo.emaps[instID][id.Key]

	return exist, nil
}

// Proof generates a proof for the round. used to satisfy interface.
func (fo *FixedRolacle) Proof(ctx context.Context, layer types.LayerID, round int32) ([]byte, error) {
	kInBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(kInBytes, uint32(round))
	hash := fnv.New32()
	if _, err := hash.Write(kInBytes); err != nil {
		log.AppLog.WithContext(ctx).With().Error("error writing hash", log.Err(err))
	}

	hashBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(hashBytes, hash.Sum32())

	return hashBytes, nil
}
