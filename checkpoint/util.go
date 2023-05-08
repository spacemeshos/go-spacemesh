package checkpoint

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/spf13/afero"

	"github.com/spacemeshos/go-spacemesh/log"
)

func ValidateSchema(data []byte) error {
	sch, err := jsonschema.CompileString(schemaFile, Schema)
	if err != nil {
		return fmt.Errorf("compile checkpoint json schema: %w", err)
	}
	var v any
	if err = json.Unmarshal(data, &v); err != nil {
		return fmt.Errorf("unmarshal checkpoint data: %w", err)
	}
	if err = sch.Validate(v); err != nil {
		return fmt.Errorf("validate checkpoint data: %w", err)
	}
	return nil
}

func savefile(logger log.Log, fs afero.Fs, dst string, data []byte) error {
	if err := fs.MkdirAll(filepath.Dir(dst), dirPerm); err != nil {
		return fmt.Errorf("create dst dir %v: %w", filepath.Dir(dst), err)
	}
	tmpf, err := afero.TempFile(fs, filepath.Dir(dst), filepath.Base(dst))
	if err != nil {
		return fmt.Errorf("%w: create tmp file", err)
	}
	logger.With().Info("created tmp file", log.String("file", tmpf.Name()))
	defer tmpf.Close()
	if _, err = tmpf.Write(data); err != nil {
		return fmt.Errorf("%w: write tmp file", err)
	}
	if err = tmpf.Sync(); err != nil {
		return fmt.Errorf("%w: sync tmp file", err)
	}
	if err = tmpf.Close(); err != nil {
		return fmt.Errorf("%w: close tmp file", err)
	}
	if err = fs.Rename(tmpf.Name(), dst); err != nil {
		return fmt.Errorf("%w: rename tmp file %v", err, dst)
	}
	logger.With().Info("created file", log.String("file", dst))
	return nil
}
