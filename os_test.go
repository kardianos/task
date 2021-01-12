package task

import "testing"

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
