// Package task handles running a sequence of tasks that may be
// conditionally added to.
package task

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/google/shlex"
)

// Action is a unit of work that gets run.
type Action interface {
	Run(ctx context.Context, st *State, sc Script) error
}

// ActionFunc runs a function like an Action.
type ActionFunc func(ctx context.Context, st *State, sc Script) error

// Run the ActionFunc.
func (f ActionFunc) Run(ctx context.Context, st *State, sc Script) error {
	return f(ctx, st, sc)
}

// Script is a list of actions.
type Script interface {
	Add(a ...Action) Script
	RunAction(ctx context.Context, st *State, a Action) error
	Run(ctx context.Context, st *State, parent Script) error
}

type script struct {
	at   int
	list []Action
}

// ScriptAdd creates a script and appends the given actions to it.
func ScriptAdd(a ...Action) Script {
	sc := &script{}
	sc.list = append(sc.list, a...)
	return sc
}

// Add creates a script if nil and appends the given actions to it.
func (sc *script) Add(a ...Action) Script {
	if sc == nil {
		sc = &script{}
	}
	sc.list = append(sc.list, a...)
	return sc
}

// Branch represents a branch condition used in Switch.
type Branch int64

// Typical branch values.
const (
	BranchUnset Branch = iota
	BranchTrue
	BranchFalse

	// BranchCustom is the smallest custom branch value that may be used.
	BranchCustom Branch = 1024
)

// Policy describes the current error policy.
type Policy byte

const (
	// PolicyFail will print to the error function and stop.
	PolicyFail Policy = iota

	// PolicyError will print to the error function and continue.
	PolicyError

	// PolicyLog will print to the log function and continue.
	PolicyLog

	// PolicyQuiet will continue execution.
	PolicyQuiet
)

// State of the current task.
type State struct {
	Env    []string
	Dir    string // Current working directory.
	Stdout io.Writer
	Stderr io.Writer
	Branch Branch
	Policy Policy
	Error  func(err error)
	Log    func(msg string)

	bucket map[string]interface{}
}

// DefaultState creates a new states with the current OS env.
func DefaultState() *State {
	wd, _ := os.Getwd()
	return &State{
		Env:    os.Environ(),
		Dir:    wd,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
}

// Filepath returns filename if absolute, or State.Dir + filename if not.
func (st *State) Filepath(filename string) string {
	if filepath.IsAbs(filename) {
		return filename
	}
	return filepath.Join(st.Dir, filename)
}

func (st *State) init() {
	if st.bucket == nil {
		st.bucket = make(map[string]interface{})
	}
}

// Get the variable called name from the state bucket.
func (st *State) Get(name string) interface{} {
	st.init()
	return st.bucket[name]
}

// Default gets the variable called name from the state bucket. If
// no value is present, return v.
func (st *State) Default(name string, v interface{}) interface{} {
	st.init()
	if got, found := st.bucket[name]; found {
		return got
	}
	return v
}

// Set the variable v to the name.
func (st *State) Set(name string, v interface{}) {
	st.init()
	st.bucket[name] = v
}

// RunAction runs the given action in the current script's context.
func (sc *script) RunAction(ctx context.Context, st *State, a Action) error {
	select {
	default:
	case <-ctx.Done():
		return ctx.Err()
	}
	err := a.Run(ctx, st, sc)
	if err == nil {
		return nil
	}
	switch st.Policy {
	default:
		return fmt.Errorf("unknown policy: %v", st.Policy)
	case PolicyFail:
		if st.Error != nil {
			st.Error(err)
		}
		return err
	case PolicyError:
		if st.Error != nil {
			st.Error(err)
		}
		return nil
	case PolicyLog:
		if st.Log != nil {
			st.Log(err.Error())
		}
		return nil
	case PolicyQuiet:
		return nil
	}
}

func (sc *script) runNext(ctx context.Context, st *State) error {
	if sc.at >= len(sc.list) {
		return io.EOF
	}
	a := sc.list[sc.at]
	sc.at++
	return sc.RunAction(ctx, st, a)
}

// Run the items in the method script. The parent script is ignored.
func (sc *script) Run(ctx context.Context, st *State, parent Script) error {
	var err error
	for {
		err = sc.runNext(ctx, st)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// Switch will run the f action, read the state branch value, and then
// execute the given action in sw.
func Switch(f Action, sw map[Branch]Action) Action {
	return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
		err := sc.RunAction(ctx, st, f)
		if err != nil {
			return err
		}
		br := st.Branch
		st.Branch = BranchUnset
		if next, ok := sw[br]; ok {
			return sc.RunAction(ctx, st, next)
		}
		return nil
	})
}

// WithPolicy sets the state policy for a single action.
func WithPolicy(p Policy, a Action) Action {
	return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
		orig := st.Policy
		st.Policy = p
		err := sc.RunAction(ctx, st, a)
		st.Policy = orig
		return err
	})
}

// Begin task actions.

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

// ExecStreamOut runs an executable but streams the output to stderr and stdout.
func ExecStreamOut(executable string, args ...string) Action {
	return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
		cmd := exec.CommandContext(ctx, executable, args...)
		cmd.Env = st.Env
		cmd.Dir = st.Dir
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

// ExecLine runs an executable, but parses the line and separates out the parts.
func ExecLine(ex ExecFunc, line string) Action {
	all, err := shlex.Split(line)
	if err != nil {
		panic(err)
	}
	if len(all) == 0 {
		panic("no values to exec")
	}
	return ex(all[0], all[1:]...)
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
		return os.Rename(st.Filepath(old), st.Filepath(new))
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
