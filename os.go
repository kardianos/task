// Copyright 2018 Daniel Theophanes. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package task

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kardianos/task/fsop"
)

// Env sets one or more environment variables.
// To delete an environment variable just include the key, no equals.
//
//	Env("GOOS=linux", "GOARCH=arm64")
func Env(env ...string) Action {
	return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
		if st.Env == nil {
			st.Env = make(map[string]string, len(env))
		}
		for i, e := range env {
			env[i] = ExpandEnv(e, st)
		}
		for _, e := range env {
			k, v, ok := strings.Cut(e, "=")
			if !ok {
				delete(st.Env, k)
				continue
			}
			st.Env[k] = v
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

// Exec runs an executable.
func Exec(executable string, args ...string) Action {
	return ExecStdin(nil, executable, args...)
}

func outputSetup(name string, std any) (func(st *State) io.Writer, func(st *State)) {
	switch s := std.(type) {
	default:
		panic(fmt.Sprintf("%s must be one of: nil, string (state name of []byte), io.Writer, *[]byte", name))
	case nil:
		return func(st *State) io.Writer {
				return nil
			}, func(st *State) {
			}
	case string:
		buf := &bytes.Buffer{}
		return func(st *State) io.Writer {
				return buf
			}, func(st *State) {
				st.Set(s, buf.Bytes())
			}
	case io.Writer:
		return func(st *State) io.Writer {
				return s
			}, func(st *State) {
			}
	case *[]byte:
		buf := &bytes.Buffer{}
		return func(st *State) io.Writer {
				return &bytes.Buffer{}
			}, func(st *State) {
				*s = buf.Bytes()
			}
	}
}

// WithStdOutErr runs the child script using adjusted stdout and stderr outputs.
// stdout and stderr may be nil, string (state name stored as []byte), io.Writer, or *[]byte.
func WithStdOutErr(stdout, stderr any, childScript Script) Action {
	outPre, outPost := outputSetup("stdout", stdout)
	errPre, errPost := outputSetup("stderr", stdout)
	return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
		oldStdout, oldStderr := st.Stdout, st.Stderr
		st.Stdout = outPre(st)
		st.Stderr = errPre(st)
		err := childScript.Run(ctx, st, sc)
		outPost(st)
		errPost(st)
		st.Stdout, st.Stderr = oldStdout, oldStderr
		return err
	})
}

// WithStdCombined runs the child script using adjusted stdout and stderr outputs.
// std may be nil, string (state name stored as []byte), io.Writer, or *[]byte.
func WithStdCombined(std any, childScript Script) Action {
	outPre, outPost := outputSetup("std", std)
	return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
		oldStdout, oldStderr := st.Stdout, st.Stderr
		w := outPre(st)
		st.Stdout = w
		st.Stderr = w
		err := childScript.Run(ctx, st, sc)
		outPost(st)
		st.Stdout, st.Stderr = oldStdout, oldStderr
		return err
	})
}

// ExecStdin runs an executable and streams the output to stderr and stdout.
// The stdin takes one of: nil, "string (state variable to []byte data), []byte, or io.Reader.
func ExecStdin(stdin any, executable string, args ...string) Action {
	var stdinReader func(st *State) io.Reader
	switch si := stdin.(type) {
	default:
		panic("stdin takes on of: nil, string (state variable to []byte), []byte, or io.Reader")
	case nil:
		stdinReader = func(st *State) io.Reader {
			return nil
		}
	case string:
		stdinReader = func(st *State) io.Reader {
			stdin, _ := st.Default(si, []byte{}).([]byte)
			if len(stdin) > 0 {
				return bytes.NewReader(stdin)
			}
			return nil
		}
	case []byte:
		stdinReader = func(st *State) io.Reader {
			return bytes.NewReader(si)
		}
	case io.Reader:
		stdinReader = func(_ *State) io.Reader {
			return si
		}
	}
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
		cmd.Stdin = stdinReader(st)
		cmd.Stdout = st.Stdout
		cmd.Stderr = st.Stderr
		err := cmd.Run()
		if err != nil {
			if ec, ok := err.(*exec.ExitError); ok {
				return fmt.Errorf("%s %q failed: %v\n%s", executable, args, err, ec.Stderr)
			}
			return err
		}
		return nil
	})
}

// WriteFile writes the given file from the input.
// Input may be a string giving the variable name of the []byte, an actual []byte, or an io.Reader.
func WriteFile(filename string, perm os.FileMode, input any) Action {
	switch i := input.(type) {
	default:
		panic("input must be one of: string ([]byte state variable name), []byte (file data), io.Reader (file data)")
	case string:
		return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
			filename = ExpandEnv(filename, st)
			return os.WriteFile(st.Filepath(filename), st.Default(i, []byte{}).([]byte), perm)
		})
	case []byte:
		return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
			filename = ExpandEnv(filename, st)
			return os.WriteFile(st.Filepath(filename), i, perm)
		})
	case io.Reader:
		return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
			filename = ExpandEnv(filename, st)
			f, err := os.OpenFile(st.Filepath(filename), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = io.Copy(f, i)
			if err != nil {
				return err
			}
			return nil
		})
	}
}

// OpenFile opens the filename and stores the file handle in file, either as in a state name (string) or as a *io.Closer.
func OpenFile(filename string, file any) Action {
	switch f := file.(type) {
	default:
		panic("file must be one of: string (state variable name), *io.Closer (file handle)")
	case string:
		return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
			filename = ExpandEnv(filename, st)
			fh, err := os.Open(st.Filepath(filename))
			if err != nil {
				return err
			}
			sc.Rollback(CloseFile(fh))

			st.Set(f, fh)
			return nil
		})
	case *io.Closer:
		return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
			filename = ExpandEnv(filename, st)
			fh, err := os.Open(st.Filepath(filename))
			if err != nil {
				return err
			}
			sc.Rollback(CloseFile(fh))

			*f = fh
			return nil
		})
	}
}

// Close the file. File may be a string (state name) or io.Closer.
func CloseFile(file any) Action {
	switch f := file.(type) {
	default:
		panic("file must be one of: string (state variable name), io.Closer (file handle)")
	case string:
		return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
			fh, ok := st.Get(f).(io.Closer)
			if !ok {
				return fmt.Errorf("state name %q is not an io.Closer, is %#v", f, fh)
			}
			st.Delete(f)

			return fh.Close()
		})
	case io.Closer:
		return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
			if f == nil {
				return nil
			}
			return f.Close()
		})
	}
}

// ReadFile reads the given file into the stdin bucket variable as a []byte.
func ReadFile(filename string, output any) Action {
	switch o := output.(type) {
	default:
		panic("output must be one of: string ([]byte state variable name), *[]byte (file data), io.Writer (file data)")
	case string:
		return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
			filename = ExpandEnv(filename, st)
			b, err := os.ReadFile(st.Filepath(filename))
			if err != nil {
				return err
			}
			st.Set(o, b)
			return nil
		})
	case *[]byte:
		return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
			filename = ExpandEnv(filename, st)
			b, err := os.ReadFile(st.Filepath(filename))
			if err != nil {
				return err
			}
			*o = b
			return nil
		})
	case io.Writer:
		return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
			filename = ExpandEnv(filename, st)
			f, err := os.Open(st.Filepath(filename))
			if err != nil {
				return err
			}
			_, err = io.Copy(o, f)
			if err != nil {
				return err
			}
			return nil
		})
	}
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
