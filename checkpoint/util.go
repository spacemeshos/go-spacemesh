package checkpoint

import (
	"bufio"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/spf13/afero"
)

type RecoveryFile struct {
	file    afero.File
	fwriter *bufio.Writer
	path    string
}

func NewRecoveryFile(fs afero.Fs, path string) (*RecoveryFile, error) {
	if err := fs.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return nil, fmt.Errorf("create dst dir %v: %w", filepath.Dir(path), err)
	}
	tmpf, err := afero.TempFile(fs, filepath.Dir(path), filepath.Base(path))
	if err != nil {
		return nil, fmt.Errorf("%w: create tmp file", err)
	}
	return &RecoveryFile{
		file:    tmpf,
		fwriter: bufio.NewWriter(tmpf),
		path:    path,
	}, nil
}

func (rf *RecoveryFile) save(fs afero.Fs) error {
	defer rf.file.Close()
	if err := rf.fwriter.Flush(); err != nil {
		return fmt.Errorf("flush tmp file: %w", err)
	}
	if err := rf.file.Sync(); err != nil {
		return fmt.Errorf("%w: sync tmp file", err)
	}
	if err := rf.file.Close(); err != nil {
		return fmt.Errorf("%w: close tmp file", err)
	}
	if err := fs.Rename(rf.file.Name(), rf.path); err != nil {
		return fmt.Errorf("%w: rename tmp file %v to %v", err, rf.file.Name(), rf.path)
	}
	return nil
}

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
