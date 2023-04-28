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

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

const (
	challengeFilename = "nipost_challenge.bin"
	builderFilename   = "nipost_builder_state.bin"
	crc64Size         = 8
)

func write(path string, data []byte) error {
	buf := bytes.NewBuffer(nil)
	checksum := crc64.New(crc64.MakeTable(crc64.ISO))
	w := io.MultiWriter(buf, checksum)
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("write data %v: %w", path, err)
	}

	crc := make([]byte, 8)
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

func saveNipostChallenge(dir string, ch *types.NIPostChallenge) error {
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("post dir not created: %w", err)
	}
	data, err := codec.Encode(ch)
	if err != nil {
		return fmt.Errorf("encode nipost challenge: %w", err)
	}
	return write(filepath.Join(dir, challengeFilename), data)
}

func loadNipostChallenge(dir string) (*types.NIPostChallenge, error) {
	filename := filepath.Join(dir, challengeFilename)
	data, err := read(filename)
	if err != nil {
		return nil, err
	}
	var ch types.NIPostChallenge
	if err = codec.Decode(data, &ch); err != nil {
		return nil, fmt.Errorf("decode nipost challenge: %w", err)
	}
	return &ch, nil
}

func discardNipostChallenge(dir string) error {
	filename := filepath.Join(dir, challengeFilename)
	if err := os.Remove(filename); err != nil {
		return fmt.Errorf("discard nipst challenge: %w", err)
	}
	return nil
}

func saveBuilderState(dir string, state *types.NIPostBuilderState) error {
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("post dir not created: %w", err)
	}
	data, err := codec.Encode(state)
	if err != nil {
		return fmt.Errorf("encode builder state: %w", err)
	}
	return write(filepath.Join(dir, builderFilename), data)
}

func loadBuilderState(dir string) (*types.NIPostBuilderState, error) {
	filename := filepath.Join(dir, builderFilename)
	data, err := read(filename)
	if err != nil {
		return nil, err
	}
	var state types.NIPostBuilderState
	if err = codec.Decode(data, &state); err != nil {
		return nil, fmt.Errorf("decode builder state: %w", err)
	}
	return &state, nil
}
