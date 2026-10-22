package main

import "testing"

func TestRunArgs(t *testing.T) {
	env := func(string) string { return "" }
	for _, args := range [][]string{nil, {"sideways"}, {"up", "extra"}} {
		if err := run(args, env); err == nil {
			t.Errorf("args %v: want usage error", args)
		}
	}
	if err := run([]string{"up"}, env); err == nil {
		t.Error("want error for missing TIERL_DATABASE_URL")
	}
	bad := func(string) string { return "postgres://u:p@127.0.0.1:1/x?sslmode=disable&connect_timeout=1" }
	if err := run([]string{"down"}, bad); err == nil {
		t.Error("want connection error")
	}
}
