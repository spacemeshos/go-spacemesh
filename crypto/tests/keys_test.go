package tests

import (
	"github.com/UnrulyOS/go-unruly/assert"
	"github.com/UnrulyOS/go-unruly/crypto"
	"github.com/UnrulyOS/go-unruly/log"
	"testing"

	libp2pcrypto "gx/ipfs/QmaPbCnUMBohSGo3KnxEa2bHqyJVVeEEcwtqJAYxerieBo/go-libp2p-crypto"

)

const (
	privKey = "4XZF1RZfBYUjL11MqsmRkRBqGqFneFHNzCS9mx9rgWq7vCbhQ"
	pubKey =  "GZsJqUpJQ73b9SwZtSUFypCeNNiPRV71tJ5rj9Cj9twNyH69tc"
)

func TestSerialization(t *testing.T) {

	priv, err := crypto.NewPrivateKeyFromString(privKey)
	if err != nil {
		t.Fatalf("failed to create priv key from string: %v", err)
	}

	pub, err := crypto.NewPublicKeyFromString(pubKey)
	if err != nil {
		t.Fatalf("failed to create priv key from string: %v", err)
	}

	privStr, err := priv.String()
	if err != nil {
		t.Fatalf("failed to get string rep of priv key: %v", err)
	}

	pubStr, err := pub.String()
	if err != nil {
		t.Fatalf("failed to get string rep of pub key: %v", err)
	}

	assert.Equal(t, privStr, privKey, "expected same string rep for pub key")
	assert.Equal(t, pubStr, pubKey, "expected same string rep for pub key")


	pubKeyFromPrivate, err := priv.GetPublicKey()
	if err != nil {
		t.Fatalf("failed to create pub key from priv: %v", err)
	}

	pubFromPrivStr, _ := pubKeyFromPrivate.String()
	assert.Equal(t, pubFromPrivStr, pubStr, "expected same public key")

}

func TestKeys(t *testing.T) {

	priv, pub, _ := crypto.GenerateKeyPair(libp2pcrypto.Secp256k1, 256)

	privStr, err := priv.String()
	if err != nil {
		t.Fatalf("failed to get string rep of priv key: %v", err)
	}

	pubStr, err := pub.String()
	if err != nil {
		t.Fatalf("failed to get string rep of pub key: %v", err)
	}

	log.Info("%s %s", privStr, pubStr)

	pubKeyData, err := pub.Bytes()
	if err != nil {
		t.Fatalf("failed to get pub key bytes: %v", err)
	}

	privKeyData, err := priv.Bytes()
	if err != nil {
		t.Fatalf("failed to get priv key bytes: %v", err)
	}

	pubKeyFromPrivate, err := priv.GetPublicKey()
	if err != nil {
		t.Fatalf("failed to create pub key from priv: %v", err)
	}

	pubFromPrivStr, err := pubKeyFromPrivate.String()
	assert.Equal(t, pubFromPrivStr, pubStr, "expected same public key")

	priv1, err := crypto.NewPrivateKey(privKeyData)
	if err != nil {
		t.Fatalf("failed to create priv key from bytes: %v", err)
	}

	priv1Str, err := priv1.String()
	if err != nil {
		t.Fatalf("failed to create get priv key string: %v", err)
	}

	assert.Equal(t, privStr, priv1Str, "expected same publick key")


	pub1, err := crypto.NewPublicKey(pubKeyData)
	if err != nil {
		t.Fatalf("failed to create pub key from bytes: %v", err)
	}

	pub1Str, err := pub1.String()
	if err != nil {
		t.Fatalf("failed to create get pub key string: %v", err)
	}

	assert.Equal(t, pubStr, pub1Str, "expected same publick key")

}
