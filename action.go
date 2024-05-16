// Copyright 2018 Daniel Theophanes. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package task handles running a sequence of tasks. State context
// is separated from script actions. Native context support.
package task

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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

// Script is a list of actions. If an action
type Script interface {
	Add(a ...Action)                                          // Add normal actions to the script.
	Rollback(a ...Action)                                     // Add actions to only  be run on rollback.
	Defer(a ...Action)                                        // Add actions to be run at the end, both on error and on normal run.
	RunAction(ctx context.Context, st *State, a Action) error // Run a single action on the script.
	Run(ctx context.Context, st *State, parent Script) error  // Run current script under givent state.
}

// Run is the entry point for actions. It is a short-cut
// for ScriptAdd and Run.
func Run(ctx context.Context, st *State, a Action) error {
	sc := NewScript(a)
	return sc.Run(ctx, st, nil)
}

type script struct {
	at   int
	list []Action

	rollback *script
}

// NewScript creates a script and appends the given actions to it.
func NewScript(a ...Action) Script {
	sc := &script{}
	sc.list = append(sc.list, a...)
	return sc
}

// Add creates a script if nil and appends the given actions to it.
func (sc *script) Add(a ...Action) {
	sc.list = append(sc.list, a...)
}

// Rollback adds actions to be done on failure.
func (sc *script) Rollback(a ...Action) {
	if sc.rollback == nil {
		sc.rollback = &script{}
	}
	sc.rollback.Add(a...)
}

// Defer executes the given actions both in the event of a rollback or
// for normal execution.
func (sc *script) Defer(a ...Action) {
	if sc.rollback == nil {
		sc.rollback = &script{}
	}
	sc.rollback.Add(a...)
	sc.Add(a...)
}

// Rollback adds actions to the current rollback script.
func Rollback(a ...Action) Action {
	return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
		sc.Rollback(a...)
		return nil
	})
}

// Defer actions to the current end of the script. Always execute on error or success.
func Defer(a ...Action) Action {
	return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
		sc.Defer(a...)
		return nil
	})
}

// Branch represents a branch condition used in Switch.
type Branch int64

// Typical branch values.
const (
	BranchUnset Branch = iota
	BranchTrue
	BranchFalse
	BranchCommit
	BranchRollback

	// BranchCustom is the smallest custom branch value that may be used.
	BranchCustom Branch = 1024
)

// Policy describes the current error policy.
type Policy byte

// Policies may be combined together. The default policy is to fail on error
// and run any rollback acitions. If Continue is selected then normal execution
// will proceed and a rollback will not be triggered. If Log is selected
// any error will be logged to the ErrorLogger. If SkipRollback is selected
// then a failure will not trigger the rollback actions. If both Continue
// and SkipRollbackk are selected, execution will continue and SkipRollback
// will be ignored.
const (
	PolicyFail     Policy = 0
	PolicyContinue Policy = 1 << iota
	PolicyLog
	PolicySkipRollback

	// Fail
	// Fail + Log
	// Fail + Log + SkipRollback
	// Fail + SkipRollback
	// Continue
	// Continue + Log
	//
	// Continue + SkipRollback will ignore skip rollback.
)

// State of the current task.
type State struct {
	Env    map[string]string
	Dir    string // Current working directory.
	Stdout io.Writer
	Stderr io.Writer
	Branch Branch
	Policy Policy

	ErrorLogger func(err error)  // Logger to use when Error is called.
	MsgLogger   func(msg string) // Logger to use when Log or Logf is called.

	bucket map[string]interface{}
}

// Values of the state.
func (st *State) Values() map[string]interface{} {
	return st.bucket
}

// Environ calls os.Environ and maps it to key value pairs.
func Environ() map[string]string {
	envList := os.Environ()
	envMap := make(map[string]string, len(envList))
	for _, env := range envList {
		ss := strings.SplitN(env, "=", 2)
		if len(ss) != 2 {
			continue
		}
		envMap[ss[0]] = ss[1]
	}
	return envMap
}

// DefaultState creates a new states with the current OS env.
func DefaultState() *State {
	wd, _ := os.Getwd()
	return &State{
		Env:    Environ(),
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

// Log a message to the MsgLogger if present.
func (st *State) Log(msg string) {
	if st.MsgLogger == nil {
		return
	}
	st.MsgLogger(msg)
}

// Logf logs a formatted message to the MsgLogger if present.
func (st *State) Logf(f string, v ...interface{}) {
	st.Log(fmt.Sprintf(f, v...))
}

// Error reports an error to the ErrorLogger if present.
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
	if sc == nil {
		return nil
	}
	select {
	default:
	case <-ctx.Done():
		return ctx.Err()
	}
	err := a.Run(ctx, st, sc)
	if err == nil {
		return nil
	}
	if st.Policy&PolicyLog != 0 {
		st.Error(err)
	}
	if st.Policy&PolicyContinue != 0 {
		err = nil
	}
	if st.Policy&PolicySkipRollback != 0 {
		return err
	}
	if err == nil {
		return err
	}
	rberr := sc.rollback.Run(context.Background(), st, sc)
	if rberr == nil {
		return err
	}
	return fmt.Errorf("%v, rollback failed: %v", err, rberr)
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
	if sc == nil {
		return nil
	}
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

// AddRollback adds rollback actions to the current Script. Rollback actions
// are only executed on failure under non-Continue policies.
func AddRollback(a ...Action) Action {
	return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
		sc.Rollback(a...)
		return nil
	})
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
