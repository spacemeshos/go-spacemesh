package node

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/gofrs/flock"
)

func lock(path string) (unlock func() error, err error) {
	lockDir := filepath.Dir(path)
	_, err = os.Stat(lockDir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		err := os.Mkdir(lockDir, os.ModePerm)
		if err != nil {
			return nil, fmt.Errorf("creating dir %s for lock %s: %w", lockDir, path, err)
		}
	case err != nil:
		return nil, fmt.Errorf("checking filelock %s directory existence: %w", lockDir, err)
	}
	fl := flock.New(path)
	locked, err := fl.TryLock()
	if err != nil {
		return nil, fmt.Errorf("trying to obtain filelock %s: %w", path, err)
	}
	if !locked {
		return nil, fmt.Errorf("only one spacemesh instance should be running (locking file %s)", fl.Path())
	}
	return fl.Unlock, nil
}
