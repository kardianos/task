package task

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

func TestExpandEnv(t *testing.T) {
	type kv = map[string]interface{}
	type ks = map[string]string
	list := []struct {
		Name   string
		State  kv
		Env    ks
		Input  string
		Output string
	}{
		{
			Name: "override",
			State: kv{
				"k1": int64(45),
			},
			Env: ks{
				"k1": "letters",
			},
			Input:  "abc${k1}xyz",
			Output: "abc45xyz",
		},
		{
			Name: "env",
			State: kv{
				"k1": int64(45),
			},
			Env: ks{
				"k2": "letters",
			},
			Input:  "abc${k2}xyz",
			Output: "abclettersxyz",
		},
	}

	for _, item := range list {
		t.Run(item.Name, func(t *testing.T) {
			st := &State{
				Env:    item.Env,
				bucket: item.State,
			}
			got := ExpandEnv(item.Input, st)
			if g, w := got, item.Output; g != w {
				t.Fatalf("got %q; want %q", g, w)
			}
		})
	}
}

func getString(varName string, value *string) Action {
	return ActionFunc(func(ctx context.Context, st *State, sc Script) error {
		switch v := st.Get(varName).(type) {
		default:
			return fmt.Errorf("unable to get value for varname %q: %#v", varName, v)
		case []byte:
			*value = strings.TrimSpace(string(v))
		case string:
			*value = strings.TrimSpace(v)
		}
		return nil
	})
}

func TestWriteStd(t *testing.T) {
	lsPath, _ := exec.LookPath("ls")
	grepPath, _ := exec.LookPath("grep")
	if len(lsPath) == 0 || len(grepPath) == 0 {
		t.Skip("missing ls or grep")
	}

	stdOut := &bytes.Buffer{}
	stdErr := &bytes.Buffer{}

	st := &State{
		Stdout: stdOut,
		Stderr: stdErr,
	}

	var result string
	sc := NewScript(
		WithStd(VAR("stdout"), VAR("stderr"), NewScript(
			Exec("ls"),
			ExecStdin(VAR("stdout"), "grep", ".mod"),
			getString("stdout", &result),
		)),
	)
	ctx := context.Background()
	err := sc.Run(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != "go.mod" {
		t.Fatalf("expected go.mod, got %q", result)
	}
	if stdOut.Len() > 0 {
		t.Fatal("stdout has data")
	}
	if stdErr.Len() > 0 {
		t.Fatal("stderr has data")
	}
}
