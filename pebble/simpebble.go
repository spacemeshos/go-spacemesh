package pebble

import (
	"bytes"
	"errors"
	"fmt"
	"log"

	"github.com/cockroachdb/pebble"
)

var ErrNotFound = errors.New("not found")

type KvDb struct {
	db *pebble.DB
}

func New(path string) *KvDb {
	db, err := pebble.Open(path, &pebble.Options{})
	if err != nil {
		log.Fatal(err)
	}

	return &KvDb{db: db}
}

func (db *KvDb) IterPrefix(prefix []byte, cb func(k, v []byte) bool) error {
	iter, err := db.db.NewIter(&pebble.IterOptions{LowerBound: prefix})
	if err != nil {
		return err
	}
	defer iter.Close()

	if !iter.SeekGE(prefix) {
		return nil
	}

	if !bytes.HasPrefix(iter.Key(), prefix) {
		return nil
	}

	for {
		stop := cb(iter.Key(), iter.Value())
		if stop {
			return nil
		}
		if !iter.Next() {
			return nil
		}
		if !bytes.HasPrefix(iter.Key(), prefix) {
			return nil
		}
	}

	return nil
}

func (db *KvDb) Has(key []byte) (bool, error) {
	iter, err := db.db.NewIter(nil)
	if err != nil {
		return false, err
	}
	defer iter.Close()

	if !iter.SeekGE(key) {
		return false, nil
	}
	if !iter.Valid() {
		return false, nil
	}
	if bytes.Equal(iter.Key(), key) {
		return true, nil
	}

	return false, nil
}

func (db *KvDb) DeleteRange(start, end []byte) error {
	return db.db.DeleteRange(start, end, pebble.Sync)
}

func (db *KvDb) DeletePrefix(prefix []byte) error {
	end := make([]byte, len(prefix))
	copy(end, prefix)
	end[len(end)-1] += 1 // increment the last byte so we could do a range delete
	return db.db.DeleteRange(prefix, end, pebble.Sync)
}

func (db *KvDb) GetVisit(key []byte, cb func(v []byte)) error {
	value, closer, err := db.db.Get(key)
	if err != nil {
		return fmt.Errorf("get: %w", err)
	}
	cb(value)
	if err := closer.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}
	return nil
}

func (db *KvDb) Get(key []byte) ([]byte, error) {
	value, closer, err := db.db.Get(key)
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get: %w", err)
	}
	ret := make([]byte, len(value))
	copy(ret, value)
	if err := closer.Close(); err != nil {
		return nil, fmt.Errorf("close: %w", err)
	}

	return ret, nil
}

func (db *KvDb) put(key, value []byte) error {
	if err := db.db.Set(key, value, pebble.NoSync); err != nil {
		return err
	}
	return nil
}

func (db *KvDb) NewBatch() *pebble.Batch {
	return db.db.NewBatch()
}

type Serializer interface {
	Serialize() (key, value []byte, err error)
}

type Deserializer interface {
	Deserialize(key, value []byte) error
}

func (db *KvDb) Set(k, v []byte, _ *pebble.WriteOptions) error {
	return db.db.Set(k, v, pebble.NoSync)
}

func (db *KvDb) Put(k, v []byte) error {
	err := db.put(k, v)
	if err != nil {
		return fmt.Errorf("put: %w", err)
	}
	return nil
}

func (db *KvDb) Close() error {
	return db.db.Close()
}
