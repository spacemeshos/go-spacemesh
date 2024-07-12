package checkpoint

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"path/filepath"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/spf13/afero"

	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
)

var (
	ErrCheckpointNotFound      = errors.New("checkpoint not found")
	ErrCheckpointRequestFailed = errors.New("checkpoint request failed")
	ErrUrlSchemeNotSupported   = errors.New("url scheme not supported")
)

type RecoveryFile struct {
	file    afero.File
	fwriter *bufio.Writer
	path    string
}

func NewRecoveryFile(aferoFs afero.Fs, path string) (*RecoveryFile, error) {
	if err := aferoFs.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return nil, fmt.Errorf("create dst dir %v: %w", filepath.Dir(path), err)
	}
	f, _ := aferoFs.Stat(path)
	if f != nil {
		return nil, fmt.Errorf("%w: file already exist: %v", fs.ErrExist, path)
	}
	tmpf, err := afero.TempFile(aferoFs, filepath.Dir(path), filepath.Base(path))
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
		return errors.New("no recovery data")
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

func httpToLocalFile(
	ctx context.Context,
	resource *url.URL,
	fs afero.Fs,
	dst string,
	retryMax int,
	retryDelay time.Duration,
) error {
	c := retryablehttp.NewClient()
	c.RetryMax = retryMax
	c.RetryWaitMin = retryDelay
	c.RetryWaitMax = retryDelay * 2
	c.Backoff = retryablehttp.LinearJitterBackoff

	req, err := retryablehttp.NewRequestWithContext(ctx, http.MethodGet, resource.String(), nil)
	if err != nil {
		return fmt.Errorf("create http request: %w", err)
	}

	resp, err := c.Do(req)
	if err != nil {
		// This shouldn't really happen. According to net/http docs for Do:
		// "Any returned error will be of type *url.Error."
		return fmt.Errorf("%w: %w", ErrCheckpointRequestFailed, err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return ErrCheckpointNotFound
	default:
		return fmt.Errorf("%w: status code %d", ErrCheckpointRequestFailed, resp.StatusCode)
	}
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

func poetProofRefs(ctx context.Context, db sql.Executor, id types.ATXID) ([]types.PoetProofRef, error) {
	var blob sql.Blob
	version, err := atxs.LoadBlob(ctx, db, id.Bytes(), &blob)
	if err != nil {
		return nil, fmt.Errorf("getting blob for %s: %w", id, err)
	}

	switch version {
	case types.AtxV1:
		var atx wire.ActivationTxV1
		if err := codec.Decode(blob.Bytes, &atx); err != nil {
			return nil, fmt.Errorf("decoding ATX blob: %w", err)
		}

		return []types.PoetProofRef{types.PoetProofRef(atx.NIPost.PostMetadata.Challenge)}, nil
	case types.AtxV2:
		var atx wire.ActivationTxV2
		if err := codec.Decode(blob.Bytes, &atx); err != nil {
			return nil, fmt.Errorf("decoding ATX blob: %w", err)
		}
		refs := make([]types.PoetProofRef, len(atx.NiPosts))
		for i, post := range atx.NiPosts {
			refs[i] = types.PoetProofRef(post.Challenge)
		}
		return refs, nil
	}
	return nil, fmt.Errorf("unsupported ATX version: %v", version)
}
