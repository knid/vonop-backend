package utils

import (
	"net/http"
	"os"
	"path/filepath"
)

type NoDirFileSystem struct {
	FS http.FileSystem
}

func (n NoDirFileSystem) Open(name string) (http.File, error) {
	f, err := n.FS.Open(name)
	if err != nil {
		return nil, err
	}

	stat, err := f.Stat()
	if err != nil {
		return nil, err
	}

	// Prevent directory listing
	if stat.IsDir() {
		index := filepath.Join(name, "index.html")
		if _, err := n.FS.Open(index); err != nil {
			return nil, os.ErrNotExist
		}
	}

	return f, nil
}
