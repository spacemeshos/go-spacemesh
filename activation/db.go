package activation

import (
	"bytes"
	"errors"
	"github.com/davecgh/go-xdr/xdr2"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/mesh"
)

type ActivationDb struct {
	//todo: think about whether we need one db or several
	Atxs database.DB
}

func (db *ActivationDb) StoreAtx(ech EpochId, atx *mesh.ActivationTx) error {
	//todo: maybe cleanup DB if failed by using defer
	if b, err := db.Atxs.Get(atx.Id().Bytes()); err == nil && len(b) > 0 {
		// exists - how should we handle this?
		return nil
	}
	b, err := mesh.AtxAsBytes(atx)
	if err != nil {
		return err
	}
	err = db.Atxs.Put(atx.Id().Bytes(), b)
	if err != nil {
		return err
	}

	err = db.addAtxToEpoch(ech, atx.Id())
	if err != nil {
		return err
	}
	err = db.addAtxToNodeId(atx.NodeId, atx.Id())
	if err != nil {
		return err
	}
	return nil
}

//todo: add tx to epoch
func (db *ActivationDb) addAtxToEpoch(epochId EpochId, atx mesh.AtxId) error {
	ids, err := db.Atxs.Get(epochId.ToBytes())
	var atxs []mesh.AtxId
	if err != nil {
		//layer doesnt exist, need to insert new layer
		ids = []byte{}
		atxs = make([]mesh.AtxId, 0, 0)
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
	return db.Atxs.Put(epochId.ToBytes(), w)
}

func (db *ActivationDb) addAtxToNodeId(nodeId mesh.NodeId, atx mesh.AtxId) error {
	ids, err := db.Atxs.Get(nodeId.ToBytes())
	var atxs []mesh.AtxId
	if err != nil {
		//layer doesnt exist, need to insert new layer
		ids = []byte{}
		atxs = make([]mesh.AtxId, 0, 0)
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
	return db.Atxs.Put(nodeId.ToBytes(), w)
}

func (db *ActivationDb) GetNodeAtxIds(node mesh.NodeId) ([]mesh.AtxId, error) {
	ids, err := db.Atxs.Get(node.ToBytes())
	if err != nil {
		return nil, err
	}
	atxs, err := decodeAtxIds(ids)
	if err != nil {
		return nil, err
	}
	return atxs, nil
}

func (db *ActivationDb) GetEpochAtxIds(epochId EpochId) ([]mesh.AtxId, error) {
	ids, err := db.Atxs.Get(epochId.ToBytes())
	if err != nil {
		return nil, err
	}
	atxs, err := decodeAtxIds(ids)
	if err != nil {
		return nil, err
	}
	return atxs, nil
}

func (db *ActivationDb) GetAtx(id mesh.AtxId) (*mesh.ActivationTx, error) {
	b, err := db.Atxs.Get(id.Bytes())
	if err != nil {
		return nil, err
	}
	atx, err := mesh.BytesAsAtx(b)
	if err != nil {
		return nil, err
	}
	return atx, nil
}

func decodeAtxIds(idsBytes []byte) ([]mesh.AtxId, error) {
	var ids []mesh.AtxId
	if _, err := xdr.Unmarshal(bytes.NewReader(idsBytes), &ids); err != nil {
		return nil, errors.New("error marshaling layer ")
	}
	return ids, nil
}

func encodeAtxIds(ids []mesh.AtxId) ([]byte, error) {
	var w bytes.Buffer
	if _, err := xdr.Marshal(&w, &ids); err != nil {
		return nil, errors.New("error marshalling atx ids ")
	}
	return w.Bytes(), nil
}
