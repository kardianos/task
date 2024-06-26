package task

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
)

func TestCommand(t *testing.T) {
	showVar := ActionFunc(func(ctx context.Context, st *State, sc Script) error {
		kk := make([]string, 0, len(st.bucket))
		for k := range st.bucket {
			kk = append(kk, k)
		}
		sort.Strings(kk)
		for _, k := range kk {
			fmt.Fprintf(st.Stdout, "var %s = %[2]v (%[2]T)\n", k, st.Get(k))
		}
		return nil
	})

	list := []struct {
		Name    string
		Command *Command
		Args    string
		ENV     map[string]string
		Output  string
		Error   string
	}{
		{
			Name: "default",
			Command: &Command{
				Name:  "cmder",
				Usage: "Example Commander",
				Flags: []*Flag{
					{Name: "f1", Usage: "set the current f1", Default: "ghi"},
					{Name: "f2", Usage: "set the current f2", Default: "nmo"},
					{Name: "f3", Usage: "set the current f3", Default: "fhg", ENV: "CMDER_F3"},
				},
				Action: showVar,
			},
			ENV:  map[string]string{},
			Args: "",
			Output: `
var f1 = ghi (string)
var f2 = nmo (string)
var f3 = fhg (string)
`,
		},
		{
			Name: "env",
			Command: &Command{
				Name:  "cmder",
				Usage: "Example Commander",
				Flags: []*Flag{
					{Name: "f1", Usage: "set the current f1", Default: "ghi"},
					{Name: "f2", Usage: "set the current f2", Default: "nmo"},
					{Name: "f3", Usage: "set the current f3", Default: "fhg", ENV: "CMDER_F3"},
				},
				Action: showVar,
			},
			ENV: map[string]string{
				"CMDER_F3": "sky",
			},
			Args: "",
			Output: `
var f1 = ghi (string)
var f2 = nmo (string)
var f3 = sky (string)
`,
		},
		{
			Name: "override",
			Command: &Command{
				Name:  "cmder",
				Usage: "Example Commander",
				Flags: []*Flag{
					{Name: "f1", Usage: "set the current f1", Default: "ghi"},
					{Name: "f2", Usage: "set the current f2", Default: "nmo"},
					{Name: "f3", Usage: "set the current f3", Default: "fhg", ENV: "CMDER_F3"},
				},
				Action: showVar,
			},
			ENV: map[string]string{
				"CMDER_F3": "sky",
			},
			Args: "-f3 box",
			Output: `
var f1 = ghi (string)
var f2 = nmo (string)
var f3 = box (string)
`,
		},
		{
			Name: "required-missing",
			Command: &Command{
				Name:  "cmder",
				Usage: "Example Commander",
				Flags: []*Flag{
					{Name: "f1", Usage: "set the current f1", Default: "ghi", Required: true},
					{Name: "f2", Usage: "set the current f2", Default: "nmo"},
					{Name: "f3", Usage: "set the current f3", Default: "fhg", ENV: "CMDER_F3", Required: true},
				},
				Action: showVar,
			},
			ENV: map[string]string{
				"CMDER_F3": "sky",
			},
			Args: "-f2 box",
			Error: `
flag "f1" required
cmder - Example Commander
	-*f1 - set the current f1 (ghi)
	-f2 - set the current f2 (nmo)
	-*f3 [CMDER_F3] - set the current f3 (fhg)
`,
		},
	}

	ts := strings.TrimSpace
	for _, item := range list {
		t.Run(item.Name, func(t *testing.T) {
			ctx := context.Background()
			stdout := &strings.Builder{}
			stderr := &strings.Builder{}
			st := &State{
				Env:    item.ENV,
				Dir:    t.TempDir(),
				Stdout: stdout,
				Stderr: stderr,
				ErrorLogger: func(err error) {
					t.Error(err)
				},
				MsgLogger: func(msg string) {
					t.Log(msg)
				},
			}
			if st.Env == nil {
				st.Env = map[string]string{}
			}
			ff := strings.Fields(item.Args)

			err := Run(ctx, st, item.Command.Exec(ff))
			var gotErr string
			if err != nil {
				gotErr = err.Error()
			}
			if g, w := ts(gotErr), ts(item.Error); g != w {
				t.Fatalf("error want:\n%s\n\ngot:\n%s\n", w, g)
			}

			if w, g := ts(item.Output), ts(stdout.String()); w != g {
				t.Fatalf("output want:\n%s\n\ngot:\n%s\n", w, g)
			}
		})
	}
}
