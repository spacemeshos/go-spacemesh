package localsql

import (
	"encoding/binary"
	"fmt"
	"hash/crc64"
	"io"
	"os"
	"path/filepath"

	"github.com/natefinch/atomic"
	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

const (
	challengeFilename = "nipost_challenge.bin"
	postFilename      = "post.bin"
)

func write(path string, data []byte) error {
	tmpName := fmt.Sprintf("%s.tmp", path)
	tmp, err := os.Create(tmpName)
	if err != nil {
		return fmt.Errorf("create temporary file %s: %w", tmpName, err)
	}
	defer tmp.Close()

	checksum := crc64.New(crc64.MakeTable(crc64.ISO))
	w := io.MultiWriter(tmp, checksum)
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("write data %v: %w", tmp.Name(), err)
	}

	crc := make([]byte, crc64.Size)
	binary.BigEndian.PutUint64(crc, checksum.Sum64())
	if _, err := tmp.Write(crc); err != nil {
		return fmt.Errorf("write checksum %s: %w", tmp.Name(), err)
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close tmp file %s: %w", tmp.Name(), err)
	}

	if err := atomic.ReplaceFile(tmp.Name(), path); err != nil {
		return fmt.Errorf("save file from %s, %s: %w", tmp.Name(), path, err)
	}

	return nil
}

func read(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open file %s: %w", path, err)
	}
	defer file.Close()

	fInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to get file info %s: %w", path, err)
	}
	if fInfo.Size() < crc64.Size {
		return nil, fmt.Errorf("file %s is too small", path)
	}

	data := make([]byte, fInfo.Size()-crc64.Size)
	checksum := crc64.New(crc64.MakeTable(crc64.ISO))
	if _, err := io.TeeReader(file, checksum).Read(data); err != nil {
		return nil, fmt.Errorf("read file %s: %w", path, err)
	}

	saved := make([]byte, crc64.Size)
	if _, err := file.Read(saved); err != nil {
		return nil, fmt.Errorf("read checksum %s: %w", path, err)
	}

	savedChecksum := binary.BigEndian.Uint64(saved)

	if savedChecksum != checksum.Sum64() {
		return nil, fmt.Errorf("wrong checksum 0x%X, computed 0x%X", savedChecksum, checksum.Sum64())
	}

	return data, nil
}

func load(filename string, dst scale.Decodable) error {
	data, err := read(filename)
	if err != nil {
		return fmt.Errorf("reading file: %w", err)
	}

	if err := codec.Decode(data, dst); err != nil {
		return fmt.Errorf("decoding: %w", err)
	}
	return nil
}

func save(filename string, src scale.Encodable) error {
	if _, err := os.Stat(filepath.Dir(filename)); err != nil {
		return err
	}
	data, err := codec.Encode(src)
	if err != nil {
		return fmt.Errorf("encoding: %w", err)
	}
	if err := write(filename, data); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}
	return nil
}

func saveNipostChallenge(dir string, ch *types.NIPostChallenge) error {
	if err := save(filepath.Join(dir, challengeFilename), ch); err != nil {
		return fmt.Errorf("saving nipost challenge: %w", err)
	}
	return nil
}

func loadNipostChallenge(dir string) (*types.NIPostChallenge, error) {
	var ch types.NIPostChallenge
	if err := load(filepath.Join(dir, challengeFilename), &ch); err != nil {
		return nil, fmt.Errorf("loading nipost challenge: %w", err)
	}
	return &ch, nil
}

func discardNipostChallenge(dir string) error {
	filename := filepath.Join(dir, challengeFilename)
	backupName := fmt.Sprintf("%s.bak", filename)
	if err := atomic.ReplaceFile(filename, backupName); err != nil {
		return fmt.Errorf("discarding nipost challenge: %w", err)
	}
	return nil
}

func savePost(dir string, post *types.Post) error {
	if err := save(filepath.Join(dir, postFilename), post); err != nil {
		return fmt.Errorf("saving post: %w", err)
	}
	return nil
}

func loadPost(dir string) (*types.Post, error) {
	var post types.Post
	if err := load(filepath.Join(dir, postFilename), &post); err != nil {
		return nil, fmt.Errorf("loading post: %w", err)
	}
	return &post, nil
}

func discardPost(dir string) error {
	filename := filepath.Join(dir, postFilename)
	backupName := fmt.Sprintf("%s.bak", filename)
	if err := atomic.ReplaceFile(filename, backupName); err != nil {
		return fmt.Errorf("discarding post: %w", err)
	}
	return nil
}
