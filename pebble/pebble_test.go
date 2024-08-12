package pebble_test

//import (
//"bytes"
//"testing"

//"github.com/spacemeshos/go-spacemesh/pebble"
//)

//func TestFlat(t *testing.T) {
//dir := t.TempDir()
//pdb := pebble.NewFlat(dir)
//k, v := []byte("key"), []byte("value")
//s := &serializer{k, v}
//if err := pdb.Put(s); err != nil {
//t.Fatal(err)
//}

//val, err := pdb.Get(k, s)
//if err != nil {
//t.Fatal(err)
//}
//if !bytes.Equal(val, v) {
//t.Fatal(val)
//}
//}

//type serializer struct {
//k, v []byte
//}

//func (s *serializer) Serialize() (key, value []byte, err error) {
//return s.k, s.v, nil
//}

//func (s *serializer) Deserialize(key, value []byte) error {
//return nil
//}
