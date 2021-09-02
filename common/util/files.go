package util

import (
	"archive/tar"
	"cloud.google.com/go/storage"
	"compress/gzip"
	"context"
	"github.com/spacemeshos/go-spacemesh/log"
	"io"
	"os"
	"path/filepath"
)

// tarxzf downloads a tar.gz file from gcloud an extract it in root
func Tarxzf(bucketName string, filename string) {
	ctx := context.Background()

	client, err := storage.NewClient(ctx)
	if err != nil {
		log.Panic("Failed to create client: %v", err)
	}
	defer client.Close()
	// Creates a Bucket instance.
	bucket := client.Bucket(bucketName)

	obj := bucket.Object(filename).ReadCompressed(true) // see https://developer.bestbuy.com/apis
	rdr, err := obj.NewReader(ctx)
	if err != nil {
		log.Panic("err reading compressed file", err)
	}
	defer rdr.Close()

	gzr, err := gzip.NewReader(rdr)
	if err != nil {
		log.Panic("err creating a new gzip reader", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()

		switch {
		// if no more files are found return
		case err == io.EOF:
			return
		// return any other error
		case err != nil:
			log.Panic("Failed to extract backup: %s", err)
			return
		// if the header is nil, just skip it (not sure how this happens)
		case header == nil:
			continue
		}

		// the target location where the dir/file should be created
		target := filepath.Join("/", header.Name)
		// the following switch could also be done using fi.Mode(), not sure if there
		// a benefit of using one vs. the other.
		// fi := header.FileInfo()

		// check the file type
		switch header.Typeflag {

		// if its a dir and it doesn't exist create it
		case tar.TypeDir:
			if _, err := os.Stat(target); err != nil {
				if err := os.MkdirAll(target, 0755); err != nil {
					log.Panic("Failed to mkdir: %s", err)
					return
				}
			}

		// if it's a file create it
		case tar.TypeReg:
			f, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
			if err != nil {
				log.Panic("Failed to create a file: %s", err)
				return
			}

			// copy over contents
			if _, err := io.Copy(f, tr); err != nil {
				log.Panic("Failed to copy archive file to disk: %s", err)
				return
			}

			// manually close here after each file operation; defering would cause each file close
			// to wait until all operations have completed.
			f.Close()
		}
	}
}