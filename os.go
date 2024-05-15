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
// The text may be VAR or string.
func ExpandEnv(text any, st *State) string {
	var stringText string
	switch v := text.(type) {
	default:
		panic(fmt.Errorf("knows VAR and string, unsupported type %#v", v))
	case VAR:
		switch v := st.Get(string(v)).(type) {
		default:
			panic(fmt.Errorf("knows VAR and string, unsupported type %#v", v))
		case string:
			stringText = v
		}
	case string:
		stringText = v
	}
	return os.Expand(stringText, func(key string) string {
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

// VAR represents a state variable name.
// When passed to a function, resolves to the state variable name.
type VAR string

func outputSetup(name string, std any) (func(st *State) io.Writer, func(st *State)) {
	switch s := std.(type) {
	default:
		panic(fmt.Sprintf("%s must be one of: nil, VAR, io.Writer, *[]byte", name))
	case nil:
		return func(st *State) io.Writer {
				return nil
			}, func(st *State) {
			}
	case VAR:
		buf := &bytes.Buffer{}
		return func(st *State) io.Writer {
				return buf
			}, func(st *State) {
				st.Set(string(s), buf.Bytes())
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
// stdout and stderr may be nil, VAR (state name stored as []byte), io.Writer, or *[]byte.
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

// Exec runs an executable.
// The executable and args may be of type VAR or string.
func Exec(executable any, args ...any) Action {
	return ExecStdin(nil, executable, args...)
}

// ExecStdin runs an executable and streams the output to stderr and stdout.
// The stdin takes one of: nil, "string (state variable to []byte data), []byte, or io.Reader.
// The executable and args may be of type VAR or string.
func ExecStdin(stdin any, executable any, args ...any) Action {
	var stdinReader func(st *State) io.Reader
	switch si := stdin.(type) {
	default:
		panic("stdin takes on of: nil, VAR (state variable to []byte), string, []byte, or io.Reader")
	case nil:
		stdinReader = func(st *State) io.Reader {
			return nil
		}
	case VAR:
		stdinReader = func(st *State) io.Reader {
			stdin, _ := st.Default(string(si), []byte{}).([]byte)
			if len(stdin) > 0 {
				return bytes.NewReader(stdin)
			}
			return nil
		}
	case string:
		stdinReader = func(st *State) io.Reader {
			return strings.NewReader(si)
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
		sExec := ExpandEnv(executable, st)
		sArgs := make([]string, len(args))
		for i, a := range args {
			sArgs[i] = ExpandEnv(a, st)
		}
		cmd := exec.CommandContext(ctx, sExec, sArgs...)
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
// Input may be a VAR, []byte, string, or io.Reader.
// The filename may be VAR or string.
func WriteFile(filename any, perm os.FileMode, input any) Action {
	switch i := input.(type) {
	default:
		panic("input must be one of: string ([]byte state variable name), []byte (file data), io.Reader (file data)")
	case VAR:
		return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
			fn := ExpandEnv(filename, st)
			fn = st.Filepath(fn)
			switch v := st.Get(string(i)).(type) {
			default:
				return fmt.Errorf("uknown type for %q: %#v", i, v)
			case []byte:
				return os.WriteFile(fn, v, perm)
			case string:
				return os.WriteFile(fn, []byte(v), perm)
			case io.Reader:
				f, err := os.OpenFile(fn, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
				if err != nil {
					return err
				}
				defer f.Close()
				_, err = io.Copy(f, v)
				if err != nil {
					return err
				}
				return nil
			}
		})
	case string:
		return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
			fn := ExpandEnv(filename, st)
			return os.WriteFile(st.Filepath(fn), []byte(i), perm)
		})
	case []byte:
		return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
			fn := ExpandEnv(filename, st)
			return os.WriteFile(st.Filepath(fn), i, perm)
		})
	case io.Reader:
		return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
			fn := ExpandEnv(filename, st)
			f, err := os.OpenFile(st.Filepath(fn), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
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
// The filename may be VAR or string.
func OpenFile(filename any, file any) Action {
	switch f := file.(type) {
	default:
		panic("file must be one of: VAR, *io.Closer (file handle)")
	case VAR:
		return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
			fn := ExpandEnv(filename, st)
			fh, err := os.Open(st.Filepath(fn))
			if err != nil {
				return err
			}
			sc.Rollback(CloseFile(fh))

			st.Set(string(f), fh)
			return nil
		})
	case *io.Closer:
		return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
			fn := ExpandEnv(filename, st)
			fh, err := os.Open(st.Filepath(fn))
			if err != nil {
				return err
			}
			sc.Rollback(CloseFile(fh))

			*f = fh
			return nil
		})
	}
}

// CloseFile closes the file. File may be a VAR or io.Closer.
func CloseFile(file any) Action {
	switch f := file.(type) {
	default:
		panic("file must be one of: string (state variable name), io.Closer (file handle)")
	case VAR:
		return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
			fh, ok := st.Get(string(f)).(io.Closer)
			if !ok {
				return fmt.Errorf("state name %q is not an io.Closer, is %#v", f, fh)
			}

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
// output may be a VAR, *string, *[]byte, or io.Writer.
// The filename may be VAR or string.
func ReadFile(filename any, output any) Action {
	switch o := output.(type) {
	default:
		panic("output must be one of: VAR, *[]byte (file data), io.Writer (file data)")
	case VAR:
		return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
			fn := ExpandEnv(filename, st)
			b, err := os.ReadFile(st.Filepath(fn))
			if err != nil {
				return err
			}
			st.Set(string(o), b)
			return nil
		})
	case *string:
		return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
			fn := ExpandEnv(filename, st)
			b, err := os.ReadFile(st.Filepath(fn))
			if err != nil {
				return err
			}
			*o = string(b)
			return nil
		})
	case *[]byte:
		return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
			fn := ExpandEnv(filename, st)
			b, err := os.ReadFile(st.Filepath(fn))
			if err != nil {
				return err
			}
			*o = b
			return nil
		})
	case io.Writer:
		return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
			fn := ExpandEnv(filename, st)
			f, err := os.Open(st.Filepath(fn))
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
// The filename may be VAR or string.
func Delete(filename any) Action {
	return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
		fn := ExpandEnv(filename, st)
		return os.RemoveAll(st.Filepath(fn))
	})
}

// Move file.
// The filenames old and new may be VAR or string.
func Move(old, new any) Action {
	return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
		fnOld := ExpandEnv(old, st)
		fnNew := ExpandEnv(new, st)
		np := st.Filepath(fnNew)
		err := os.MkdirAll(filepath.Dir(np), 0700)
		if err != nil {
			return err
		}
		return os.Rename(st.Filepath(fnOld), np)
	})
}

// Copy file or folder recursively. If only is present, only copy path
// if only returns true.
// The filenames old and new may be VAR or string.
func Copy(old, new any, only func(p string, st *State) bool) Action {
	return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
		fnOld := ExpandEnv(old, st)
		fnNew := ExpandEnv(new, st)
		return fsop.Copy(st.Filepath(fnOld), st.Filepath(fnNew), func(p string) bool {
			if only == nil {
				return true
			}
			return only(p, st)
		})
	})
}
