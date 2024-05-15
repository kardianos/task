// Copyright 2018 Daniel Theophanes. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package task_test

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/kardianos/task"
)

func ExampleCommand() {
	showVar := func(name string) task.Action {
		return task.ActionFunc(func(ctx context.Context, st *task.State, sc task.Script) error {
			st.Log(fmt.Sprintf("var %s = %[2]v (%[2]T)", name, st.Get(name)))
			return nil
		})
	}

	cmd := &task.Command{
		Name:  "cmder",
		Usage: "Example Commander",
		Flags: []*task.Flag{
			{Name: "f1", Usage: "set the current f1", Default: "ghi"},
			{Name: "f2", Usage: "set the current f2", Default: "nmo"},
			{Name: "f3", Usage: "set the current f3", Default: "fhg", ENV: "CMDER_F3"},
		},
		Commands: []*task.Command{
			{Name: "run1", Usage: "run the first one here", Action: task.NewScript(
				task.ExecStdin("ps", "-A"),
				task.ExecStdin("ls", "-la"),
			)},
			{
				Name: "run2", Usage: "run the second one here",
				Flags: []*task.Flag{
					{Name: "tf", Default: false, Type: task.FlagBool},
				},
				Action: task.NewScript(
					showVar("f1"),
					showVar("f2"),
					showVar("f3"),
					showVar("tf"),
				),
			},
		},
	}

	args := []string{"-f1", "xyz", "run2", "-tf"} // os.Args[1:]

	st := task.DefaultState()
	st.Env = map[string]string{
		"CMDER_F3": "sky",
	}
	ctx := context.Background()
	err := task.Start(ctx, time.Second*3, func(ctx context.Context) error {
		err := task.Run(ctx, st, cmd.Exec([]string{"-help"}))
		fmt.Fprintf(os.Stdout, "%s", err)
		fmt.Println("---")
		return task.Run(ctx, st, cmd.Exec(args))
	})
	if err != nil {
		fmt.Fprint(os.Stderr, err)
		os.Exit(1)
	}

	// Output:
	// invalid flag -help
	// cmder - Example Commander
	// 	-f1 - set the current f1 (ghi)
	// 	-f2 - set the current f2 (nmo)
	// 	-f3 [CMDER_F3] - set the current f3 (fhg)
	//
	// 	run1 - run the first one here
	// 	run2 - run the second one here
	// ---
	// var f1 = xyz (string)
	// var f2 = nmo (string)
	// var f3 = sky (string)
	// var tf = true (bool)

}

func ExampleCommand_Flags() {
	showVar := func(name string) task.Action {
		return task.ActionFunc(func(ctx context.Context, st *task.State, sc task.Script) error {
			st.Log(fmt.Sprintf("var %s = %[2]v (%[2]T)", name, st.Get(name)))
			return nil
		})
	}

	cmd := &task.Command{
		Name:  "cmder",
		Usage: "Example Commander",
		Flags: []*task.Flag{
			{Name: "f1", Usage: "set the current f1", Default: "ghi"},
		},
		Commands: []*task.Command{
			{Name: "run1", Usage: "run the first one here", Commands: []*task.Command{
				{Name: "run1b"},
			}, Action: task.NewScript(
				showVar("f1"),
				showVar("args"),
			)},
			{
				Name: "run2", Usage: "run the second one here",
				Flags: []*task.Flag{
					{Name: "tf", Default: false, Type: task.FlagBool},
				},
				Action: task.NewScript(
					showVar("f1"),
					showVar("args"),
				),
			},
		},
	}

	err := task.Start(context.Background(), time.Second*3, func(ctx context.Context) error {
		args := []string{"-f1", "xyz1", "run2", "abc123"} // os.Args[1:]
		err := task.Run(ctx, task.DefaultState(), cmd.Exec(args))
		if err != nil {
			return err
		}
		fmt.Println("---")

		// The "--" is required to pass arguments to commands that have sub-commands.
		args = []string{"-f1", "xyz2", "run1", "--", "lights"} // os.Args[1:]
		return task.Run(ctx, task.DefaultState(), cmd.Exec(args))
	})
	if err != nil {
		fmt.Fprint(os.Stderr, err)
		os.Exit(1)
	}

	// Output:
	// var f1 = xyz1 (string)
	// var args = [abc123] ([]string)
	// ---
	// var f1 = xyz2 (string)
	// var args = [lights] ([]string)

}
