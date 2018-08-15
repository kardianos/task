// Copyright 2018 Daniel Theophanes. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package task

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Command represents an Action that may be invoked with a name.
// Flags will be mapped to State bucket values.
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
	Name    string
	Usage   string
	Default interface{}
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
	used bool
	flag *Flag
}

func (fs *flagStatus) init() error {
	fl := fs.flag
	if fl.Default == nil {
		return nil
	}
	// If a default value is set, automatically set the flag type.
	if fl.Type == FlagAuto {
		switch fl.Default.(type) {
		case string:
			fl.Type = FlagString
		case int64:
			fl.Type = FlagInt64
		case int32:
			fl.Type = FlagInt64
		case int:
			fl.Type = FlagInt64
		case float64:
			fl.Type = FlagFloat64
		case float32:
			fl.Type = FlagFloat64
		case time.Duration:
			fl.Type = FlagDuration
		}
	}
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
	return nil
}

func (fs *flagStatus) set(st *State, vs string) error {
	fl := fs.flag
	if fs.used {
		return fmt.Errorf("flag -%s already declared", fl.Name)
	}
	fs.used = true
	switch fl.Type {
	default:
		return fmt.Errorf("unknown flag type %v", fl.Type)
	case FlagString, FlagAuto:
		st.Set(fl.Name, vs)
	case FlagBool:
		if vs == "" {
			st.Set(fl.Name, true)
		} else {
			v, err := strconv.ParseBool(vs)
			if err != nil {
				return err
			}
			st.Set(fl.Name, v)
		}
	case FlagInt64:
		v, err := strconv.ParseInt(vs, 10, 64)
		if err != nil {
			return err
		}
		st.Set(fl.Name, v)
	case FlagFloat64:
		v, err := strconv.ParseFloat(vs, 64)
		if err != nil {
			return err
		}
		st.Set(fl.Name, v)
	case FlagDuration:
		v, err := time.ParseDuration(vs)
		if err != nil {
			return err
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
			flagLookup[fl.Name] = fs
		}
		// First parse any flags.
		// The first non-flag seen is a sub-command, stop after the cmd is found.
		var nextFlag *flagStatus
		for len(args) > 0 {
			a := args[0]
			args = args[1:]

			if nextFlag != nil {
				if err := nextFlag.set(st, a); err != nil {
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
				for _, fs := range flagLookup {
					if fs.used {
						continue
					}
					fs.setDefault(st)
				}
				// This is a subcommand.
				cmd, ok := cmdLookup[a]
				if !ok {
					return c.helpError("invalid command %q", a)
				}
				return cmd.Exec(args).Run(ctx, st, sc)
			}
			// This is a flag.
			nameValue := strings.SplitN(a[1:], "=", 2)
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
			if err := fl.set(st, val); err != nil {
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
		return c.Action.Run(ctx, st, sc)
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
