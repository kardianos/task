package task_test

import (
	"context"
	"fmt"
	"os"

	"bitbucket.org/kardianos/task"
)

func ExampleMain() {
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
			{Name: "f1", Usage: "set the current f1"},
		},
		Commands: []*task.Command{
			{Name: "run1", Usage: "run the first one here", Action: task.ScriptAdd(
				task.ExecLine(task.ExecStreamOut, "ps -A"),
				task.ExecLine(task.ExecStreamOut, "ls -la"),
			)},
			{
				Name: "run2", Usage: "run the second one here",
				Flags: []*task.Flag{
					{Name: "tf", Default: false, Type: task.FlagBool},
				},
				Action: task.ScriptAdd(
					showVar("f1"),
					showVar("tf"),
				),
			},
		},
	}

	args := []string{"-f1", "xyz", "run2", "-tf"} // os.Args[1:]

	st := task.DefaultState()
	sc := task.ScriptAdd(cmd.Exec(args))
	ctx := context.Background()

	err := sc.Run(ctx, st, nil)
	if err != nil {
		fmt.Fprint(os.Stderr, err)
		os.Exit(1)
	}

	// Output:
	// var f1 = xyz (string)
	// var tf = true (bool)
}
