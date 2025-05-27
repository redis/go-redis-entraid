package main

import "testing"

func TestRunScript(t *testing.T) {
	got := RunScript()
	want := "OK"
	if got != want {
		t.Errorf("RunScript() = %q, want %q", got, want)
	}
}
