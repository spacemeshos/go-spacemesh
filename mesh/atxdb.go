package mesh

import (
	"bytes"
	"errors"
	"github.com/davecgh/go-xdr/xdr2"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
)

const CounterKey = 0xaaaa

type ActivationDb struct {
	//todo: think about whether we need one db or several
	atxs database.DB
}

func NewActivationDb(dbstore database.DB) *ActivationDb {
	return &ActivationDb{atxs: dbstore}
}

func (db *ActivationDb) StoreAtx(ech EpochId, atx *ActivationTx) error {
	log.Debug("storing atx %v, in epoch %v", atx, ech)

	//todo: maybe cleanup DB if failed by using defer
	if b, err := db.atxs.Get(atx.Id().Bytes()); err == nil && len(b) > 0 {
		// exists - how should we handle this?
		return nil
	}
	b, err := AtxAsBytes(atx)
	if err != nil {
		return err
	}
	err = db.atxs.Put(atx.Id().Bytes(), b)
	if err != nil {
		return err
	}

	err = db.addAtxToEpoch(ech, atx.Id())
	if err != nil {
		return err
	}
	if atx.Valid() {
		db.incValidAtxCounter(ech)
	}
	err = db.addAtxToNodeId(atx.NodeId, atx.Id())
	if err != nil {
		return err
	}
	return nil
}

func epochCounterKey(ech EpochId) []byte {
	return append(ech.ToBytes(), common.Uint64ToBytes(uint64(CounterKey))...)
}

func (db *ActivationDb) incValidAtxCounter(ech EpochId) error {
	key := epochCounterKey(ech)
	val, err := db.atxs.Get(key)
	if err == nil {
		return db.atxs.Put(key, common.Uint64ToBytes(common.BytesToUint64(val)+1))
	}
	return db.atxs.Put(key, common.Uint64ToBytes(1))
}

func (db *ActivationDb) ActiveIds(ech EpochId) uint64 {
	key := epochCounterKey(ech)
	val, err := db.atxs.Get(key)
	if err != nil {
		return 0
	}
	return common.BytesToUint64(val)
}

func (db *ActivationDb) addAtxToEpoch(epochId EpochId, atx AtxId) error {
	ids, err := db.atxs.Get(epochId.ToBytes())
	var atxs []AtxId
	if err != nil {
		//epoch doesnt exist, need to insert new layer
		ids = []byte{}
		atxs = make([]AtxId, 0, 0)
	} else {
		atxs, err = decodeAtxIds(ids)
		if err != nil {
			return errors.New("could not get all atxs from database ")
		}
	}
	atxs = append(atxs, atx)
	w, err := encodeAtxIds(atxs)
	if err != nil {
		return errors.New("could not encode layer atx ids")
	}
	return db.atxs.Put(epochId.ToBytes(), w)
}

func (db *ActivationDb) addAtxToNodeId(nodeId NodeId, atx AtxId) error {
	ids, err := db.atxs.Get(nodeId.ToBytes())
	var atxs []AtxId
	if err != nil {
		//layer doesnt exist, need to insert new layer
		ids = []byte{}
		atxs = make([]AtxId, 0, 0)
	} else {
		atxs, err = decodeAtxIds(ids)
		if err != nil {
			return errors.New("could not get all atxs from database ")
		}
	}
	atxs = append(atxs, atx)
	w, err := encodeAtxIds(atxs)
	if err != nil {
		return errors.New("could not encode layer atx ids")
	}
	return db.atxs.Put(nodeId.ToBytes(), w)
}

func (db *ActivationDb) GetNodeAtxIds(node NodeId) ([]AtxId, error) {
	ids, err := db.atxs.Get(node.ToBytes())
	if err != nil {
		return nil, err
	}
	atxs, err := decodeAtxIds(ids)
	if err != nil {
		return nil, err
	}
	return atxs, nil
}

func (db *ActivationDb) GetEpochAtxIds(epochId EpochId) ([]AtxId, error) {
	ids, err := db.atxs.Get(epochId.ToBytes())
	if err != nil {
		return nil, err
	}
	atxs, err := decodeAtxIds(ids)
	if err != nil {
		return nil, err
	}
	return atxs, nil
}

func (db *ActivationDb) GetAtx(id AtxId) (*ActivationTx, error) {
	b, err := db.atxs.Get(id.Bytes())
	if err != nil {
		return nil, err
	}
	atx, err := BytesAsAtx(b)
	if err != nil {
		return nil, err
	}
	return atx, nil
}

func decodeAtxIds(idsBytes []byte) ([]AtxId, error) {
	var ids []AtxId
	if _, err := xdr.Unmarshal(bytes.NewReader(idsBytes), &ids); err != nil {
		return nil, errors.New("error marshaling layer ")
	}
	return ids, nil
}

func encodeAtxIds(ids []AtxId) ([]byte, error) {
	var w bytes.Buffer
	if _, err := xdr.Marshal(&w, &ids); err != nil {
		return nil, errors.New("error marshalling atx ids ")
	}
	return w.Bytes(), nil
}
