// Copyright 2018 Daniel Theophanes. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package fsop has common file system operations.
package fsop

import (
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
)

// Only takes a path and returns true to include the file or folder.
type Only func(p string) bool

// Copy the the oldpath to the newpath. If only is not nil, only copy the
// files and folders where only returns true.
func Copy(oldpath, newpath string, only Only) error {
	if only != nil && !only(oldpath) {
		return nil
	}
	fi, err := os.Stat(oldpath)
	if err != nil {
		return err
	}
	if fi.IsDir() {
		return copyFolder(fi, oldpath, newpath, only)
	}
	return copyFile(fi, oldpath, newpath)
}

func copyFile(fi os.FileInfo, oldpath, newpath string) error {
	old, err := os.Open(oldpath)
	if err != nil {
		return err
	}
	defer old.Close()

	err = os.MkdirAll(filepath.Dir(newpath), fi.Mode()|0700)
	if err != nil {
		return err
	}

	new, err := os.OpenFile(newpath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, fi.Mode())
	if err != nil {
		return err
	}
	_, err = io.Copy(new, old)
	cerr := new.Close()
	if cerr != nil {
		return cerr
	}

	return err
}

func copyFolder(fi os.FileInfo, oldpath, newpath string, only Only) error {
	err := os.MkdirAll(newpath, fi.Mode())
	if err != nil {
		return err
	}
	list, err := ioutil.ReadDir(oldpath)
	if err != nil {
		return err
	}

	for _, item := range list {
		err = Copy(filepath.Join(oldpath, item.Name()), filepath.Join(newpath, item.Name()), only)
		if err != nil {
			return err
		}
	}
	return nil
}
