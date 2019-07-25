package eligibility

import (
	"encoding/binary"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/types"
	"hash/fnv"
	"sync"
)

type FixedRolacle struct {
	honest map[string]struct{}
	faulty map[string]struct{}
	emaps  map[uint32]map[string]struct{}
	mutex  sync.Mutex
	mapRW  sync.RWMutex
}

func New() *FixedRolacle {
	rolacle := &FixedRolacle{}
	rolacle.honest = make(map[string]struct{})
	rolacle.faulty = make(map[string]struct{})
	rolacle.emaps = make(map[uint32]map[string]struct{})

	return rolacle
}

func (fo *FixedRolacle) IsIdentityActiveOnConsensusView(edId string, layer types.LayerID) (bool, error) {
	return true, nil
}

func (fo *FixedRolacle) Export(id uint32, committeeSize int) map[string]struct{} {
	fo.mapRW.RLock()
	total := len(fo.honest) + len(fo.faulty)
	fo.mapRW.RUnlock()

	// normalize committee size
	size := committeeSize
	if committeeSize > total {
		log.Warning("committee size bigger than the number of clients. Expected %v<=%v", committeeSize, total)
		size = total
	}

	fo.mapRW.Lock()
	// generate if not exist for the requested K
	if _, exist := fo.emaps[id]; !exist {
		fo.emaps[id] = fo.generateEligibility(size)
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

func (fo *FixedRolacle) Register(isHonest bool, client string) {
	if isHonest {
		fo.update(fo.honest, client)
	} else {
		fo.update(fo.faulty, client)
	}
}

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

func (fo *FixedRolacle) generateEligibility(expCom int) map[string]struct{} {
	emap := make(map[string]struct{}, expCom)

	if expCom == 0 {
		return emap
	}

	expHonest := expCom/2 + 1
	if expHonest > len(fo.honest) {
		log.Warning("Not enough registered honest. Expected %v<=%v", expHonest, len(fo.honest))
		expHonest = len(fo.honest)
	}

	hon := cloneMap(fo.honest)
	pickUnique(expHonest, hon, emap)

	expFaulty := expCom - expHonest
	if expFaulty > len(fo.faulty) {
		if len(fo.faulty) > 0 { // not enough
			log.Debug("Not enough registered dishonest to pick from. Expected %v<=%v. Picking %v instead", expFaulty, len(fo.faulty), len(fo.faulty))
		} else { // no faulty at all - acceptable
			log.Debug("No registered dishonest to pick from. Picking honest instead")
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
	h.Write(instanceID.ToBytes())
	h.Write(kInBytes)

	return h.Sum32()
}

func (fo *FixedRolacle) Eligible(layer types.LayerID, round int32, committeeSize int, id types.NodeId, sig []byte) (bool, error) {
	fo.mapRW.RLock()
	total := len(fo.honest) + len(fo.faulty) // safe since len >= 0
	fo.mapRW.RUnlock()

	// normalize committee size
	size := committeeSize
	if committeeSize > total {
		log.Warning("committee size bigger than the number of clients. Expected %v<=%v", committeeSize, total)
		size = total
	}

	instId := hashLayerAndRound(layer, round)

	fo.mapRW.Lock()
	// generate if not exist for the requested K
	if _, exist := fo.emaps[instId]; !exist {
		fo.emaps[instId] = fo.generateEligibility(size)
	}
	fo.mapRW.Unlock()
	// get eligibility result
	_, exist := fo.emaps[instId][id.Key]

	return exist, nil
}

func (fo *FixedRolacle) Proof(id types.NodeId, layer types.LayerID, round int32) ([]byte, error) {
	kInBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(kInBytes, uint32(round))
	hash := fnv.New32()
	hash.Write([]byte(id.Key))
	hash.Write(kInBytes)

	hashBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(hashBytes, uint32(hash.Sum32()))

	return hashBytes, nil
}
