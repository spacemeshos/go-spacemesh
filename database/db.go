package database

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
)

type DB interface {
	Put(key, value []byte) error
	Get(key []byte) (value []byte, err error)
	Close()
}

type LevelDB struct {
	db *leveldb.DB
	wo *opt.WriteOptions
	ro *opt.ReadOptions
}

func (db LevelDB) Close() {
	db.db.Close()
}

func (db LevelDB) Put(key, value []byte) error {
	return db.db.Put(key, value, db.wo)
}

func (db LevelDB) Get(key []byte) (value []byte, err error) {
	return db.db.Get(key, db.ro)
}

func NewLevelDbStore(name string, wo *opt.WriteOptions, ro *opt.ReadOptions) DB {
	blocks, err := leveldb.OpenFile("../database/data/"+name, nil)
	if err != nil {
		log.Error("could not create "+name+" database ", err)
		panic("failed to open database")
	}
	return LevelDB{blocks, wo, ro}
}
