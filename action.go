// Package task handles running a sequence of tasks that may be
// conditionally added to.
package task

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	// PolicyFail will stop execution.
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

	ErrorLogger func(err error)
	MsgLogger   func(msg string)

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
		ErrorLogger: func(err error) {
			fmt.Fprint(os.Stderr, err, "\n")
		},
		MsgLogger: func(msg string) {
			fmt.Fprint(os.Stdout, msg, "\n")
		},
	}
}

func (st *State) Log(msg string) {
	if st.MsgLogger == nil {
		return
	}
	st.MsgLogger(msg)
}
func (st *State) Error(err error) {
	if st.ErrorLogger == nil {
		return
	}
	st.ErrorLogger(err)
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

// Delete the variable called name.
func (st *State) Delete(name string) {
	st.init()
	delete(st.bucket, name)
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
		return err
	case PolicyError:
		st.Error(err)
		return nil
	case PolicyLog:
		st.Log(err.Error())
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
