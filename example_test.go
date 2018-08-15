// Copyright 2018 Daniel Theophanes. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package task_test

import (
	"context"
	"fmt"
	"os"

	"bitbucket.org/kardianos/task"
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
		},
		Commands: []*task.Command{
			{Name: "run1", Usage: "run the first one here", Action: task.ScriptAdd(
				task.ExecStreamOut("ps", "-A"),
				task.ExecStreamOut("ls", "-la"),
			)},
			{
				Name: "run2", Usage: "run the second one here",
				Flags: []*task.Flag{
					{Name: "tf", Default: false, Type: task.FlagBool},
				},
				Action: task.ScriptAdd(
					showVar("f1"),
					showVar("f2"),
					showVar("tf"),
				),
			},
		},
	}

	args := []string{"-f1", "xyz", "run2", "-tf"} // os.Args[1:]

	st := task.DefaultState()
	sc := task.ScriptAdd(cmd.Exec(args))
	ctx := context.Background()

	fmt.Fprintf(os.Stdout, cmd.Exec([]string{"-help"}).Run(ctx, st, nil).Error())
	fmt.Println("---")
	err := sc.Run(ctx, st, nil)
	if err != nil {
		fmt.Fprint(os.Stderr, err)
		os.Exit(1)
	}

	// Output:
	// invalid flag -help
	// cmder - Example Commander
	// 	-f1 - set the current f1 (ghi)
	// 	-f2 - set the current f2 (nmo)
	//
	// 	run1 - run the first one here
	// 	run2 - run the second one here
	// ---
	// var f1 = xyz (string)
	// var f2 = nmo (string)
	// var tf = true (bool)

}
