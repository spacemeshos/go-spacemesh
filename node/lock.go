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
	if _, err := os.Stat(lockDir); errors.Is(err, fs.ErrNotExist) {
		err := os.Mkdir(lockDir, os.ModePerm)
		if err != nil {
			return nil, fmt.Errorf("creating dir %s for lock %s: %w", lockDir, path, err)
		}
	}
	fl := flock.New(path)
	locked, err := fl.TryLock()
	if err != nil {
		return nil, fmt.Errorf("flock %s: %w", path, err)
	} else if !locked {
		return nil, fmt.Errorf("only one spacemesh instance should be running (locking file %s)", fl.Path())
	}
	return fl.Unlock, nil
}
