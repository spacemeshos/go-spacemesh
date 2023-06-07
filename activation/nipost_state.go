package activation

import (
	"bytes"
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
	builderFilename   = "nipost_builder_state.bin"
	postFilename      = "post.bin"
	crc64Size         = 8
)

func write(path string, data []byte) error {
	buf := bytes.NewBuffer(nil)
	checksum := crc64.New(crc64.MakeTable(crc64.ISO))
	w := io.MultiWriter(buf, checksum)
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("write data %v: %w", path, err)
	}

	crc := make([]byte, crc64Size)
	binary.BigEndian.PutUint64(crc, checksum.Sum64())
	if _, err := buf.Write(crc); err != nil {
		return fmt.Errorf("write checksum %v: %w", path, err)
	}
	if err := atomic.WriteFile(path, buf); err != nil {
		return fmt.Errorf("save file %v: %w", path, err)
	}
	return nil
}

func read(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file %v: %w", path, err)
	}
	checksum := crc64.New(crc64.MakeTable(crc64.ISO))
	end := len(data) - crc64Size
	if _, err = checksum.Write(data[:end]); err != nil {
		return nil, fmt.Errorf("read checksum %v: %w", path, err)
	}
	saved := binary.BigEndian.Uint64(data[end:])
	if saved != checksum.Sum64() {
		return nil, fmt.Errorf("wrong checksum %d, computed %d", saved, checksum.Sum64())
	}
	return data[:end], nil
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
	if err := os.Remove(filename); err != nil {
		return fmt.Errorf("discarding nipost challenge: %w", err)
	}
	return nil
}

func saveBuilderState(dir string, state *types.NIPostBuilderState) error {
	if err := save(filepath.Join(dir, builderFilename), state); err != nil {
		return fmt.Errorf("saving builder state: %w", err)
	}
	return nil
}

func loadBuilderState(dir string) (*types.NIPostBuilderState, error) {
	var state types.NIPostBuilderState
	if err := load(filepath.Join(dir, builderFilename), &state); err != nil {
		return nil, fmt.Errorf("loading builder state: %w", err)
	}
	return &state, nil
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
