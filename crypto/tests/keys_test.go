package tests

//
//import (
//	"github.com/UnrulyOS/go-unruly/assert"
//	"github.com/UnrulyOS/go-unruly/crypto"
//	"github.com/UnrulyOS/go-unruly/log"
//	"testing"
//)
//
//const (
//	privKey = "4XZF1YWXhkigALWMZHGuiHdEKrQPi4z5Eu4TETFJh56Aoygt1"
//	pubKey  = "GZsJqUSGnMiXvZ66DAABJGisocqj4gmkn3PmcB4XSRNurwNqya"
//	id1     = "Qma8AuFNHETeTNx9xYsdSU99HnW6QGvjG8JcobYsxtHWsr"
//)
//
//func TestSerialization(t *testing.T) {
//
//	priv, err := crypto.NewPrivateKeyFromString(privKey)
//	if err != nil {
//		t.Fatalf("failed to create priv key from string: %v", err)
//	}
//
//	pub, err := crypto.NewPublicKeyFromString(pubKey)
//	if err != nil {
//		t.Fatalf("failed to create priv key from string: %v", err)
//	}
//
//	privStr, err := priv.String()
//	if err != nil {
//		t.Fatalf("failed to get string rep of priv key: %v", err)
//	}
//
//	log.Info("%d", len(privStr))
//
//	pubStr, err := pub.String()
//	if err != nil {
//		t.Fatalf("failed to get string rep of pub key: %v", err)
//	}
//
//	assert.Equal(t, privStr, privKey, "expected same string rep for pub key")
//	assert.Equal(t, pubStr, pubKey, "expected same string rep for pub key")
//
//	pubKeyFromPrivate, err := priv.GetPublicKey()
//	if err != nil {
//		t.Fatalf("failed to create pub key from priv: %v", err)
//	}
//
//	pubFromPrivStr, _ := pubKeyFromPrivate.String()
//	assert.Equal(t, pubFromPrivStr, pubStr, "expected same public key")
//
//	id, _ := pub.IdFromPubKey()
//	idStr := id.String()
//
//	assert.Equal(t, id1, idStr, "expected same id")
//
//}
//
//func TestKeys(t *testing.T) {
//
//	priv, pub, _ := crypto.GenerateKeyPair()
//
//	privStr, err := priv.String()
//	if err != nil {
//		t.Fatalf("failed to get string rep of priv key: %v", err)
//	}
//
//	pubStr, err := pub.String()
//	if err != nil {
//		t.Fatalf("failed to get string rep of pub key: %v", err)
//	}
//
//	pubKeyData, err := pub.Bytes()
//	if err != nil {
//		t.Fatalf("failed to get pub key bytes: %v", err)
//	}
//
//	privKeyData, err := priv.Bytes()
//	if err != nil {
//		t.Fatalf("failed to get priv key bytes: %v", err)
//	}
//
//	pubKeyFromPrivate, err := priv.GetPublicKey()
//	if err != nil {
//		t.Fatalf("failed to create pub key from priv: %v", err)
//	}
//
//	pubFromPrivStr, err := pubKeyFromPrivate.String()
//	assert.Equal(t, pubFromPrivStr, pubStr, "expected same public key")
//
//	priv1, err := crypto.NewPrivateKey(privKeyData)
//	if err != nil {
//		t.Fatalf("failed to create priv key from bytes: %v", err)
//	}
//
//	priv1Str, err := priv1.String()
//	if err != nil {
//		t.Fatalf("failed to create get priv key string: %v", err)
//	}
//
//	assert.Equal(t, privStr, priv1Str, "expected same public key")
//
//	pub1, err := crypto.NewPublicKey(pubKeyData)
//	if err != nil {
//		t.Fatalf("failed to create pub key from bytes: %v", err)
//	}
//
//	pub1Str, err := pub1.String()
//	if err != nil {
//		t.Fatalf("failed to create get pub key string: %v", err)
//	}
//
//	assert.Equal(t, pubStr, pub1Str, "expected same publick key")
//
//}
