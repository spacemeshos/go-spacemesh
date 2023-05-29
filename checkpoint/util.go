package checkpoint

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

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
	f, _ := fs.Stat(path)
	if f != nil {
		return nil, fmt.Errorf("%w: file already exist: %v", os.ErrExist, path)
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

func (rf *RecoveryFile) Copy(fs afero.Fs, src io.Reader) error {
	n, err := io.Copy(rf.fwriter, src)
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("no recovery data")
	}
	return rf.Save(fs)
}

func (rf *RecoveryFile) Save(fs afero.Fs) error {
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

func CopyFile(fs afero.Fs, src, dst string) error {
	rf, err := NewRecoveryFile(fs, dst)
	if err != nil {
		return fmt.Errorf("new recovery file %w", err)
	}
	srcf, err := fs.Open(src)
	if err != nil {
		return fmt.Errorf("open src recovery file: %w", err)
	}
	defer srcf.Close()
	return rf.Copy(fs, srcf)
}

func httpToLocalFile(ctx context.Context, resource *url.URL, fs afero.Fs, dst string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, resource.String(), nil)
	if err != nil {
		return fmt.Errorf("create http request: %w", err)
	}
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return fmt.Errorf("http get bootstrap file: %w", err)
	}
	defer resp.Body.Close()
	rf, err := NewRecoveryFile(fs, dst)
	if err != nil {
		return fmt.Errorf("new recovery file %w", err)
	}
	return rf.Copy(fs, resp.Body)
}

func backupRecovery(fs afero.Fs, recoveryDir string) (string, error) {
	if _, err := fs.Stat(recoveryDir); err != nil {
		return "", nil
	}
	backupDir := fmt.Sprintf("%s.%d", recoveryDir, time.Now().UnixNano())
	if err := fs.Rename(recoveryDir, backupDir); err != nil {
		return "", fmt.Errorf("backup old checkpoint data: %w", err)
	}
	return backupDir, nil
}

func backupOldDb(fs afero.Fs, srcDir, dbFile string) (string, error) {
	backupDir := filepath.Join(srcDir, fmt.Sprintf("%s.%d", "backup", time.Now().Unix()))
	if err := fs.MkdirAll(backupDir, dirPerm); err != nil {
		return "", fmt.Errorf("create backup dir: %w", err)
	}
	// sqlite create .sql, .sql-shm and .sql-wal files.
	files, err := afero.Glob(fs, filepath.Join(srcDir, fmt.Sprintf("%s*", dbFile)))
	if err != nil {
		return "", fmt.Errorf("list db files: %w", err)
	}
	if len(files) == 0 {
		return "", nil
	}
	for _, src := range files {
		dst := filepath.Join(backupDir, filepath.Base(src))
		if err = fs.Rename(src, dst); err != nil {
			return "", err
		}
	}
	return backupDir, nil
}
