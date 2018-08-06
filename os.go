// Copyright 2018 Daniel Theophanes. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package task

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Env sets one or more environment variables.
//    Env("GOOS=linux", "GOARCH=arm64")
func Env(env ...string) Action {
	return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
		key := func(s string) string {
			return strings.SplitN(s, "=", 2)[0]
		}
		var out []string
		for _, item := range env {
			out = append(out, item)
		}
		for _, stItem := range st.Env {
			osKey := key(stItem)
			found := false
			for _, item := range env {
				itemKey := key(item)
				if itemKey == osKey {
					found = true
					break
				}
			}
			if !found {
				out = append(out, stItem)
			}
		}
		st.Env = out
		return nil
	})
}

// ExecFunc is the standard executable function type.
type ExecFunc func(executable string, args ...string) Action

// Exec runs an executable. Sets the "stdout" bucket variable as a []byte.
func Exec(executable string, args ...string) Action {
	return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
		cmd := exec.CommandContext(ctx, executable, args...)
		cmd.Env = st.Env
		cmd.Dir = st.Dir
		stdin, _ := st.Default("stdin", []byte{}).([]byte)
		if len(stdin) > 0 {
			cmd.Stdin = bytes.NewReader(stdin)
		}
		out, err := cmd.Output()
		st.Set("success", cmd.ProcessState.Success())
		st.Set("stdout", out)
		if err != nil {
			if ec, ok := err.(*exec.ExitError); ok {
				return fmt.Errorf("%s %q failed: %v\n%s", executable, args, err, ec.Stderr)
			}
			return err
		}
		return nil
	})
}

// Pipe sets stdin to the value of stdout. The stdout is removed.
var Pipe = ActionFunc(pipe)

func pipe(ctx context.Context, st *State, sc Script) error {
	stdin := []byte{}
	if stdout, is := st.Default("stdout", []byte{}).([]byte); is {
		stdin = stdout
	}
	st.Set("stdin", stdin)
	st.Delete("stdout")
	return nil
}

// ExecStreamOut runs an executable but streams the output to stderr and stdout.
func ExecStreamOut(executable string, args ...string) Action {
	return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
		cmd := exec.CommandContext(ctx, executable, args...)
		cmd.Env = st.Env
		cmd.Dir = st.Dir
		stdin, _ := st.Default("stdin", []byte{}).([]byte)
		if len(stdin) > 0 {
			cmd.Stdin = bytes.NewReader(stdin)
		}
		cmd.Stdout = st.Stdout
		cmd.Stderr = st.Stderr
		err := cmd.Run()
		st.Set("success", cmd.ProcessState.Success())
		if err != nil {
			if ec, ok := err.(*exec.ExitError); ok {
				return fmt.Errorf("%s %q failed: %v\n%s", executable, args, err, ec.Stderr)
			}
			return err
		}
		return nil
	})
}

// WriteFileStdout writes the given file from the "stdout" bucket variable assuming it is a []byte.
func WriteFileStdout(filename string, mode os.FileMode) Action {
	return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
		return ioutil.WriteFile(st.Filepath(filename), st.Default("stdout", []byte{}).([]byte), mode)
	})
}

// Delete file.
func Delete(filename string) Action {
	return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
		return os.RemoveAll(st.Filepath(filename))
	})
}

// Move file.
func Move(old, new string) Action {
	return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
		np := st.Filepath(new)
		err := os.MkdirAll(filepath.Dir(np), 0700)
		if err != nil {
			return err
		}
		return os.Rename(st.Filepath(old), np)
	})
}

// Copy file or folder recursively. If only is present, only copy path
// if only returns true.
func Copy(old, new string, only func(p string) bool) Action {
	return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
		return copyFileFolder(st.Filepath(old), st.Filepath(new), only)
	})
}

func copyFileFolder(oldpath, newpath string, only func(p string) bool) error {
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

func copyFolder(fi os.FileInfo, oldpath, newpath string, only func(p string) bool) error {
	err := os.MkdirAll(newpath, fi.Mode())
	if err != nil {
		return err
	}
	list, err := ioutil.ReadDir(oldpath)
	if err != nil {
		return err
	}

	for _, item := range list {
		err = copyFileFolder(filepath.Join(oldpath, item.Name()), filepath.Join(newpath, item.Name()), only)
		if err != nil {
			return err
		}
	}
	return nil
}
