// Copyright 2018 Daniel Theophanes. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package task

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kardianos/task/fsop"
)

// Env sets one or more environment variables.
// To delete an environment variable just include the key, no equals.
//    Env("GOOS=linux", "GOARCH=arm64")
func Env(env ...string) Action {
	return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
		if st.Env == nil {
			st.Env = make(map[string]string, len(env))
		}
		for i, e := range env {
			env[i] = ExpandEnv(e, st)
		}
		for _, e := range env {
			ss := strings.SplitN(e, "=", 2)
			if len(ss) != 2 {
				delete(st.Env, ss[0])
				continue
			}
			st.Env[ss[0]] = ss[1]
		}
		return nil
	})
}

// ExpandEnv will expand env vars from s and return the combined string.
// Var names may take the form of "text${var}suffix".
// The source of the value will first look for current state bucket,
// then in the state Env.
func ExpandEnv(s string, st *State) string {
	return os.Expand(s, func(key string) string {
		if st.bucket != nil {
			if v, ok := st.bucket[key]; ok {
				switch x := v.(type) {
				case string:
					return x
				case nil:
					// Nothing.
				default:
					return fmt.Sprint(x)
				}
			}
		}
		return st.Env[key]
	})
}

// ExecFunc is the standard executable function type.
type ExecFunc func(executable string, args ...string) Action

// Exec runs an executable. Sets the "stdout" bucket variable as a []byte.
func Exec(executable string, args ...string) Action {
	return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
		executable = ExpandEnv(executable, st)
		for i, a := range args {
			args[i] = ExpandEnv(a, st)
		}
		cmd := exec.CommandContext(ctx, executable, args...)
		envList := make([]string, 0, len(st.Env))
		for key, value := range st.Env {
			envList = append(envList, key+"="+value)
		}
		cmd.Env = envList
		cmd.Dir = st.Dir
		stdin, _ := st.Default("stdin", []byte{}).([]byte)
		if len(stdin) > 0 {
			cmd.Stdin = bytes.NewReader(stdin)
		}
		out, err := cmd.Output()
		st.Set("stdout", out)
		if err != nil {
			st.Set("success", false)
			if ec, ok := err.(*exec.ExitError); ok {
				return fmt.Errorf("%s %q failed: %v\n%s", executable, args, err, ec.Stderr)
			}
			return err
		}
		st.Set("success", true)
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
		executable = ExpandEnv(executable, st)
		for i, a := range args {
			args[i] = ExpandEnv(a, st)
		}
		cmd := exec.CommandContext(ctx, executable, args...)
		envList := make([]string, 0, len(st.Env))
		for key, value := range st.Env {
			envList = append(envList, key+"="+value)
		}
		cmd.Env = envList
		cmd.Dir = st.Dir
		stdin, _ := st.Default("stdin", []byte{}).([]byte)
		if len(stdin) > 0 {
			cmd.Stdin = bytes.NewReader(stdin)
		}
		cmd.Stdout = st.Stdout
		cmd.Stderr = st.Stderr
		err := cmd.Run()
		if err != nil {
			st.Set("success", false)
			if ec, ok := err.(*exec.ExitError); ok {
				return fmt.Errorf("%s %q failed: %v\n%s", executable, args, err, ec.Stderr)
			}
			return err
		}
		st.Set("success", true)
		return nil
	})
}

// WriteFileStdout writes the given file from the "stdout" bucket variable assuming it is a []byte.
func WriteFileStdout(filename string, mode os.FileMode) Action {
	return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
		filename = ExpandEnv(filename, st)
		return ioutil.WriteFile(st.Filepath(filename), st.Default("stdout", []byte{}).([]byte), mode)
	})
}

// ReadFileStdin reads the given file into the stdin bucket variable as a []byte.
func ReadFileStdin(filename string, mode os.FileMode) Action {
	return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
		filename = ExpandEnv(filename, st)
		b, err := ioutil.ReadFile(st.Filepath(filename))
		if err != nil {
			return err
		}
		st.Set("stdin", b)
		return nil
	})
}

// Delete file.
func Delete(filename string) Action {
	return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
		filename = ExpandEnv(filename, st)
		return os.RemoveAll(st.Filepath(filename))
	})
}

// Move file.
func Move(old, new string) Action {
	return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
		old = ExpandEnv(old, st)
		new = ExpandEnv(new, st)
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
func Copy(old, new string, only func(p string, st *State) bool) Action {
	return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
		old = ExpandEnv(old, st)
		new = ExpandEnv(new, st)
		return fsop.Copy(st.Filepath(old), st.Filepath(new), func(p string) bool {
			if only == nil {
				return true
			}
			return only(p, st)
		})
	})
}
