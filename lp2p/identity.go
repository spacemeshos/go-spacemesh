package lp2p

import (
	"crypto/rand"
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/libp2p/go-libp2p-core/crypto"
)

const keyFilename = "p2p.key"

func genIdentity() (crypto.PrivKey, error) {
	pk, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		return nil, err
	}
	return pk, nil
}

func ensureIdentity(dir string) (crypto.PrivKey, error) {
	// TODO add crc check
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, keyFilename)
	data, err := ioutil.ReadFile(path)
	if err == nil {
		return crypto.UnmarshalPrivateKey(data)
	}
	if errors.Is(err, os.ErrNotExist) {
		key, err := genIdentity()
		if err != nil {
			return nil, err
		}
		data, err = crypto.MarshalPrivateKey(key)
		if err != nil {
			return nil, err
		}
		if err := ioutil.WriteFile(path, data, 0o644); err != nil {
			return nil, err
		}
		return key, nil
	}
	return nil, err
}
