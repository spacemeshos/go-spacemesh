package pebble

//import (
//"fmt"
//"log"

//"github.com/cockroachdb/pebble"
//)

//const (
//NS_ATXCACHE = iota + 1
//NS_SOMETHING
//)

//type PebbleDb struct {
//db        *pebble.DB
//namespace uint8
//flat      bool // if we're flat - there's no need to encode the ns byte to keys
//}

//func NewFlat(path string) *PebbleDb {
//db, err := pebble.Open(path, &pebble.Options{})
//if err != nil {
//log.Fatal(err)
//}

//return &PebbleDb{db: db, flat: true}
//}

//func New(path string, ns uint8) *PebbleDb {
//db, err := pebble.Open(path, &pebble.Options{})
//if err != nil {
//log.Fatal(err)
//}

//return &PebbleDb{db: db, namespace: ns}
//}

//func (db *PebbleDb) Get(key []byte, out Deserializer) ([]byte, error) {
//var k []byte
//if db.flat {
//k = make([]byte, len(key)+1)
//k[0] = db.namespace
//copy(k[1:], key)
//} else {
//k = key
//}
//value, closer, err := db.db.Get(k)
//if err != nil {
//return nil, fmt.Errorf("get: %w", err)
//}
//ret := make([]byte, len(value))
//copy(ret, value)
//if err := closer.Close(); err != nil {
//return nil, fmt.Errorf("close: %w", err)
//}

//return ret, nil
//}

// func (db *PebbleDb) put(key, value []byte) error {
// k := make([]byte, len(key)+1)
// k[0] = db.namespace
// copy(k[1:], key)

//if err := db.db.Set(k, value, pebble.Sync); err != nil {
//return err
//}
//db.db.Flush()
//return nil
//}

//type Serializer interface {
//Serialize() (key, value []byte, err error)
//}

//type Deserializer interface {
//Deserialize(key, value []byte) error
//}

//func (db *PebbleDb) Put(s Serializer) error {
//k, v, err := s.Serialize()
//if err != nil {
//return fmt.Errorf("serialize: %w", err)
//}
//err = db.put(k, v)
//if err != nil {
//return fmt.Errorf("put: %w", err)
//}
//return nil
//}

//func (db *PebbleDb) Close() error {
//return db.db.Close()
//}
