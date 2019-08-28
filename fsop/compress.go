package fsop

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Compress will create and zip archive of the file(s) and folder(s) in fileOrDir.
// fileOrDir may be a single file or a directory containing many files.
// The returned bytes is the content of the zip archive.
func Compress(fileOrDir string, only Only) ([]byte, error) {
	buf := &bytes.Buffer{}
	w := zip.NewWriter(buf)

	baseStat, err := os.Stat(fileOrDir)
	if err != nil {
		return nil, err
	}
	if baseStat.IsDir() {
		err = compressDir(fileOrDir, w, only)
	} else {
		filename := fileOrDir
		fileOrDir, _ = filepath.Split(fileOrDir)
		err = compressFile(filename, fileOrDir, w, baseStat)
	}
	if err != nil {
		return nil, err
	}

	// Close the zip writer.
	err = w.Close()
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

var slashReplace = strings.NewReplacer(`\`, `/`)

func compressFile(path, baseDir string, w *zip.Writer, info os.FileInfo) error {
	// Make sure the contents of the file can be read before
	// adding it to the zip archive.
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to read file %q: %v", path, err)
	}
	defer f.Close()

	// Create the file location in the zip archive
	fh := &zip.FileHeader{
		Name:     slashReplace.Replace(strings.TrimPrefix(path, baseDir)),
		Method:   zip.Deflate,
		Modified: info.ModTime(),
	}
	fh.SetMode(info.Mode())
	zf, err := w.CreateHeader(fh)
	if err != nil {
		return fmt.Errorf("failed to create file %q in archive: %v", path, err)
	}

	// Write the contents of the file to the zip archive
	_, err = io.Copy(zf, f)
	if err != nil {
		return fmt.Errorf("failed to write contents of file %q to archive: %v", path, err)
	}
	return nil
}

// compressDir will create and zip archive of the file(s) and folder(s) in baseDir
// The returned bytes is the content of the zip archive.
func compressDir(baseDir string, w *zip.Writer, only Only) error {
	return filepath.Walk(baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("failure access path %q: %v", path, err)
		}
		if only != nil && !only(path) {
			return nil
		}

		// No need to process diretories. They will be created in the archive
		// relative a files location on disk.
		if info.IsDir() {
			return nil
		}
		return compressFile(path, baseDir, w, info)
	})
}
