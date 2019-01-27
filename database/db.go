package database

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
)

type DB interface {
	Put(key, value []byte) error
	Get(key []byte) (value []byte, err error)
	Delete(key []byte) error
	Iterator() Iterator
	Close()
}

type Iterator interface {
	Next() bool
	Key() []byte
	Error() error
	Release()
}

type LevelDB struct {
	*leveldb.DB
	wo *opt.WriteOptions
	ro *opt.ReadOptions
}

func (db LevelDB) Close() {
	db.DB.Close()
}

func (db LevelDB) Put(key, value []byte) error {
	return db.DB.Put(key, value, db.wo)
}

func (db LevelDB) Get(key []byte) (value []byte, err error) {
	return db.DB.Get(key, db.ro)
}

func (db LevelDB) Delete(key []byte) error {
	return db.DB.Delete(key, db.wo)
}

func (db LevelDB) Iterator() Iterator {
	return db.DB.NewIterator(nil, nil)
}

func NewLevelDbStore(name string, wo *opt.WriteOptions, ro *opt.ReadOptions) DB {
	blocks, err := leveldb.OpenFile("../database/data/"+name, nil)
	if err != nil {
		log.Error("could not create "+name+" database ", err)
		panic("failed to open database")
	}
	return LevelDB{blocks, wo, ro}
}
