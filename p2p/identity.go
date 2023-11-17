package p2p

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
)

const keyFilename = "p2p.key"

type identityInfo struct {
	Key []byte
	ID  peer.ID // this is needed only to simplify integration with some testing tools
}

func genIdentity() (crypto.PrivKey, error) {
	pk, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ed25519 identity: %w", err)
	}
	return pk, nil
}

func identityInfoFromDir(dir string) (*identityInfo, error) {
	path := filepath.Join(dir, keyFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file %s: %w", path, err)
	}
	var info identityInfo
	err = json.Unmarshal(data, &info)
	if err != nil {
		return nil, fmt.Errorf("unmarshal file content from %s into %+v: %w", path, info, err)
	}
	return &info, nil
}

// PrettyIdentityInfoFromDir returns a printable ID from a given identity directory.
func PrettyIdentityInfoFromDir(dir string) (string, error) {
	identityInfo, err := identityInfoFromDir(dir)
	return identityInfo.ID.String(), err
}

// EnsureIdentity generates an identity key file in given directory.
func EnsureIdentity(dir string) (crypto.PrivKey, error) {
	// TODO add crc check
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("ensure that directory %s exist: %w", dir, err)
	}
	info, err := identityInfoFromDir(dir)
	if err == nil {
		pk, err := crypto.UnmarshalPrivateKey(info.Key)
		if err != nil {
			return nil, fmt.Errorf("unmarshal privkey: %w", err)
		}
		return pk, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		key, err := genIdentity()
		if err != nil {
			return nil, err
		}
		id, err := peer.IDFromPrivateKey(key)
		if err != nil {
			panic("generated key is malformed")
		}
		raw, err := crypto.MarshalPrivateKey(key)
		if err != nil {
			panic("generated key can't be marshaled to bytes")
		}
		data, err := json.Marshal(identityInfo{
			Key: raw,
			ID:  id,
		})
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(dir, keyFilename), data, 0o644); err != nil {
			return nil, fmt.Errorf("write identity data: %w", err)
		}
		return key, nil
	}
	return nil, fmt.Errorf("read key from disk: %w", err)
}
