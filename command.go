// Copyright 2018 Daniel Theophanes. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package task

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Command represents an Action that may be invoked with a name.
// Flags will be mapped to State bucket values.
// Extra arguments at the end of a command chain will be passed to the state as
// "args []string". To pass arguments to a command that has sub-commands, first
// pass in "--" then pass in the arguments.
//
//	exec cmd arg1 arg2 # cmd has no sub-commands.
//	exec cmd -- arg1 arg2 # cmd has one or more sub-commands.
type Command struct {
	Name     string
	Usage    string
	Flags    []*Flag
	Commands []*Command
	Action   Action
}

// Flag represents values that may be set on comments.
// Values will be mapped to State bucket values.
type Flag struct {
	Name    string // Name of the flag.
	ENV     string // Optional env var to read from if flag not present.
	Usage   string
	Value   any
	Default any
	Type    FlagType
}

// FlagType is set in Flag and determins how the value is parsed.
type FlagType byte

// FlagType options. If a default is present the flag type may be left as
// Auto to choose the parse type based on the default type.
const (
	FlagAuto FlagType = iota
	FlagString
	FlagBool
	FlagInt64
	FlagFloat64
	FlagDuration
)

func (ft FlagType) spaceValue() bool {
	switch ft {
	default:
		return true
	case FlagBool:
		return false
	}
}

type flagStatus struct {
	flag *Flag
	used bool
	env  bool
}

func flagType(v any) FlagType {
	switch v.(type) {
	default:
		return FlagAuto
	case string, *string:
		return FlagString
	case bool, *bool:
		return FlagBool
	case int64, *int64:
		return FlagInt64
	case int32, *int32:
		return FlagInt64
	case int, *int:
		return FlagInt64
	case float64, *float64:
		return FlagFloat64
	case float32, *float32:
		return FlagFloat64
	case time.Duration, *time.Duration:
		return FlagDuration
	}
}

func (fs *flagStatus) init() error {
	fl := fs.flag
	// If a default value is set, automatically set the flag type.
	if fl.Type == FlagAuto && fl.Value != nil {
		fl.Type = flagType(fl.Value)
	}
	if fl.Type == FlagAuto && fl.Default != nil {
		fl.Type = flagType(fl.Default)
	}
	if fl.Default != nil {
		var ok bool
		switch fl.Type {
		default:
			return fmt.Errorf("unknown flag type %v", fl.Type)
		case FlagString:
			_, ok = fl.Default.(string)
		case FlagBool:
			_, ok = fl.Default.(bool)
		case FlagInt64:
			switch v := fl.Default.(type) {
			case int32:
				fl.Default = int64(v)
				ok = true
			case int:
				fl.Default = int64(v)
				ok = true
			case int64:
				ok = true
			}
		case FlagFloat64:
			switch v := fl.Default.(type) {
			case float32:
				fl.Default = float64(v)
				ok = true
			case float64:
				ok = true
			}
		case FlagDuration:
			_, ok = fl.Default.(time.Duration)
		}
		if !ok {
			return fmt.Errorf("invalid default flag value %[1]v (%[1]T) for -%[2]s", fl.Default, fl.Name)
		}
	}
	if fl.Value != nil {
		var ok bool
		switch fl.Type {
		default:
			return fmt.Errorf("unknown flag type %v", fl.Type)
		case FlagString:
			_, ok = fl.Value.(*string)
		case FlagBool:
			_, ok = fl.Value.(*bool)
		case FlagInt64:
			switch fl.Value.(type) {
			case *int32:
				ok = true
			case *int:
				ok = true
			case *int64:
				ok = true
			}
		case FlagFloat64:
			switch fl.Value.(type) {
			case *float32:
				ok = true
			case *float64:
				ok = true
			}
		case FlagDuration:
			_, ok = fl.Default.(*time.Duration)
		}
		if !ok {
			return fmt.Errorf("invalid default flag value %[1]v (%[1]T) for -%[2]s", fl.Default, fl.Name)
		}
	}
	return nil
}

func (fs *flagStatus) set(st *State, vs string, fromENV bool) error {
	fl := fs.flag
	if fs.used {
		setFromENV := !fromENV && fs.env
		if !setFromENV {
			return fmt.Errorf("flag -%s already declared", fl.Name)
		}
	}
	fs.used = true
	if fromENV {
		fs.env = true
	}
	switch fl.Type {
	default:
		return fmt.Errorf("unknown flag type %v", fl.Type)
	case FlagAuto:
		st.Set(fl.Name, vs)
	case FlagString:
		if x, ok := fl.Value.(*string); ok {
			*x = vs
		}
		st.Set(fl.Name, vs)
	case FlagBool:
		if vs == "" {
			if x, ok := fl.Value.(*bool); ok {
				*x = true
			}
			st.Set(fl.Name, true)
		} else {
			v, err := strconv.ParseBool(vs)
			if err != nil {
				return err
			}
			if x, ok := fl.Value.(*bool); ok {
				*x = v
			}
			st.Set(fl.Name, v)
		}
	case FlagInt64:
		v, err := strconv.ParseInt(vs, 10, 64)
		if err != nil {
			return err
		}
		switch x := fl.Value.(type) {
		case *int32:
			*x = int32(v)
		case *int:
			*x = int(v)
		case *int64:
			*x = int64(v)
		}
		st.Set(fl.Name, v)
	case FlagFloat64:
		v, err := strconv.ParseFloat(vs, 64)
		if err != nil {
			return err
		}
		switch x := fl.Value.(type) {
		case *float32:
			*x = float32(v)
		case *float64:
			*x = float64(v)
		}
		st.Set(fl.Name, v)
	case FlagDuration:
		v, err := time.ParseDuration(vs)
		if err != nil {
			return err
		}
		switch x := fl.Value.(type) {
		case *time.Duration:
			*x = v
		}
		st.Set(fl.Name, v)
	}
	return nil
}

func (fs *flagStatus) setDefault(st *State) {
	fl := fs.flag
	if fl.Default == nil {
		return
	}
	st.Set(fl.Name, fl.Default)
}

// Exec takes a command arguments and returns an Action, ready to be run.
func (c *Command) Exec(args []string) Action {
	return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
		if sc == nil {
			return errors.New("missing Script")
		}
		flagLookup := make(map[string]*flagStatus)
		cmdLookup := make(map[string]*Command)
		for _, cmd := range c.Commands {
			cmdLookup[cmd.Name] = cmd
		}
		for _, fl := range c.Flags {
			fs := &flagStatus{flag: fl}
			if err := fs.init(); err != nil {
				return err
			}
			if len(fs.flag.ENV) > 0 {
				if v, ok := st.Env[fs.flag.ENV]; ok && len(v) > 0 {
					if err := fs.set(st, v, true); err != nil {
						return err
					}
				}
			}
			flagLookup[fl.Name] = fs
		}

		// First parse any flags.
		// The first non-flag seen is a sub-command, stop after the cmd is found.
		var nextFlag *flagStatus
		for len(args) > 0 {
			a := args[0]
			prevArgs := args
			args = args[1:]

			if nextFlag != nil {
				if err := nextFlag.set(st, a, false); err != nil {
					return err
				}
				nextFlag.used = true
				nextFlag = nil
				continue
			}

			if len(a) == 0 {
				continue
			}
			if a[0] != '-' {
				if len(cmdLookup) == 0 {
					// This is an argument.
					st.Set("args", prevArgs)
					break
				}
				// This is a subcommand.
				for _, fs := range flagLookup {
					if fs.used {
						continue
					}
					fs.setDefault(st)
				}
				cmd, ok := cmdLookup[a]
				if !ok {
					return c.helpError("invalid command %q", a)
				}
				sc.Add(cmd.Exec(args))
				return nil
			}
			a = a[1:]
			if a == "-" { // "--"
				st.Set("args", args)
				break
			}
			// This is a flag.
			nameValue := strings.SplitN(a, "=", 2)
			fl, ok := flagLookup[nameValue[0]]
			if !ok {
				return c.helpError("invalid flag -%s", nameValue[0])
			}
			val := ""
			if len(nameValue) == 1 {
				if fl.flag.Type.spaceValue() {
					nextFlag = fl
					continue
				}
			} else {
				val = nameValue[1]
			}
			if err := fl.set(st, val, false); err != nil {
				return err
			}
		}
		for _, fs := range flagLookup {
			if fs.used {
				continue
			}
			fs.setDefault(st)
		}
		if nextFlag != nil {
			return fmt.Errorf("expected value after flag %q", nextFlag.flag.Name)
		}
		if c.Action == nil {
			return c.helpError("incorrect command")
		}
		sc.Add(c.Action)
		return nil
	})
}

// ErrUsage signals that the error returned is not a runtime error
// but a usage message.
type ErrUsage string

func (err ErrUsage) Error() string {
	return string(err)
}

func (c *Command) helpError(f string, v ...interface{}) error {
	msg := &strings.Builder{}
	if len(f) > 0 {
		fmt.Fprintf(msg, f, v...)
		msg.WriteRune('\n')
	}
	msg.WriteString(c.Name)
	if len(c.Usage) > 0 {
		msg.WriteString(" - ")
		msg.WriteString(c.Usage)
	}
	msg.WriteString("\n")
	for _, fl := range c.Flags {
		msg.WriteString("\t")
		msg.WriteRune('-')
		msg.WriteString(fl.Name)
		if len(fl.ENV) > 0 {
			msg.WriteString(" [")
			msg.WriteString(fl.ENV)
			msg.WriteString("]")
		}
		if len(fl.Usage) > 0 {
			msg.WriteString(" - ")
			msg.WriteString(fl.Usage)
		}
		if fl.Default != nil {
			fmt.Fprintf(msg, " (%v)", fl.Default)
		}
		msg.WriteString("\n")
	}
	msg.WriteString("\n")
	for _, sub := range c.Commands {
		msg.WriteString("\t")
		msg.WriteString(sub.Name)
		if len(sub.Usage) > 0 {
			msg.WriteString(" - ")
			msg.WriteString(sub.Usage)
		}
		msg.WriteString("\n")
	}
	return ErrUsage(msg.String())
}
