package post

import (
	"bufio"
	"github.com/spacemeshos/go-spacemesh/log"
	"os"
	"path"
)

// dataFile is a simple binary data file
type dataFile interface {
	exists() bool
	delete() error
	create() error
	close() error
	read(off int64, out []byte) error // read len(out) bytes at offset off from the file
	write(data []byte) error          // write data at current offset and advanced offset
	seek(off int64) error             // set new write offset from 0
	sync() error                      // sync all changes to store
}

type dataFileImpl struct {
	name   string
	file   *os.File
	writer *bufio.Writer
	offset int64
}

func newDataFile(dir string) dataFile {
	fileName := path.Join(dir, "post.dat")
	f := &dataFileImpl{name: fileName}
	return f
}

func (d *dataFileImpl) seek(off int64) error {

	if off == d.offset { // we are already here
		return nil
	}

	// flush any existing buffered data
	err := d.writer.Flush()
	if err != nil {
		return err
	}

	// seek to new offset
	_, err = d.file.Seek(off, 0)
	if err != nil {
		return err
	}

	d.offset = off

	// create buffered writer at current offset
	d.writer = bufio.NewWriter(d.file)
	return nil
}

func (d *dataFileImpl) write(data []byte) error {
	_, err := d.writer.Write(data)
	d.offset = d.offset + int64(len(data))
	return err
}

func (d *dataFileImpl) sync() error {
	return d.writer.Flush()
}

func (d *dataFileImpl) read(off int64, out []byte) error {
	_, err := d.file.ReadAt(out, off)
	return err
}

func (d *dataFileImpl) exists() bool {
	_, err := os.Stat(d.name)
	if err == nil {
		return true
	} else if os.IsNotExist(err) {
		return false
	} else {
		// stats error
		return false
	}
}

func (d *dataFileImpl) delete() error {
	return os.Remove(d.name)
}

func (d *dataFileImpl) close() error {
	if d.file == nil {
		return nil
	}
	return d.file.Close()
}

func (d *dataFileImpl) create() error {
	file, err := os.Create(d.name)
	if err == nil {
		d.file = file
		d.writer = bufio.NewWriter(file)
	}
	log.Debug("Created table file at: %s", d.name)
	return err
}
