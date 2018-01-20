package post

import (
	"os"
	"path"
)

// todo: support buffered writing
type dataFile interface {
	exists() bool
	delete() error
	create() error
	close() error
	read(off int64, out []byte) error   // read len(out) bytes at offset off from the file
	write(off int64, data []byte) error // write data at offset off
	sync() error
}

type dataFileImpl struct {
	name string
	file *os.File
}

func newDataFile(dir string, id string) dataFile {
	fileName := path.Join(dir, id+".post")
	f := &dataFileImpl{name: fileName}
	return f
}

func (d *dataFileImpl) write(off int64, data []byte) error {
	_, err := d.file.WriteAt(data, off)
	if err != nil {
		return err
	}

	// todo: for now sync on every write - soon: use buffered writers for sequential writes
	return d.file.Sync()
}

func (d *dataFileImpl) sync() error {
	return d.file.Sync()
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
	}
	return err
}
