//go:build windows

package main

import "testing"

func TestStartupCommandQuotesExecutablePath(t *testing.T) {
	got := startupCommand(`C:\Program Files\Sidebox\sidebox.exe`)
	want := `"C:\Program Files\Sidebox\sidebox.exe"`
	if got != want {
		t.Fatalf("startupCommand() = %q, want %q", got, want)
	}
}
