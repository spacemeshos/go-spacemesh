package activation

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

const (
	challengeFilename = "NIPostChallenge"
	builderFilename   = "NIPostBuilderState"
	ownerReadWrite    = os.FileMode(0o600)
)

func saveNipostChallenge(dir string, ch *types.NIPostChallenge) error {
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("post dir not created: %w", err)
	}
	data, err := codec.Encode(ch)
	if err != nil {
		return fmt.Errorf("encode nipost challenge: %w", err)
	}
	err = os.WriteFile(filepath.Join(dir, challengeFilename), data, ownerReadWrite)
	if err != nil {
		return fmt.Errorf("write nipost challenge: %w", err)
	}
	return nil
}

func loadNipostChallenge(dir string) (*types.NIPostChallenge, error) {
	filename := filepath.Join(dir, challengeFilename)
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("read nipst challenge: %w", err)
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
	err = os.WriteFile(filepath.Join(dir, builderFilename), data, ownerReadWrite)
	if err != nil {
		return fmt.Errorf("write builder state: %w", err)
	}
	return nil
}

func loadBuilderState(dir string) (*types.NIPostBuilderState, error) {
	filename := filepath.Join(dir, builderFilename)
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("read builder state: %w", err)
	}
	var state types.NIPostBuilderState
	if err = codec.Decode(data, &state); err != nil {
		return nil, fmt.Errorf("decode builder state: %w", err)
	}
	return &state, nil
}
