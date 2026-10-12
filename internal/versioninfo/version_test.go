package versioninfo

import "testing"

func TestString(t *testing.T) {
	originalVersion := Version
	originalCommit := Commit
	t.Cleanup(func() {
		Version = originalVersion
		Commit = originalCommit
	})

	Version = "v0.1.0"
	Commit = "abc1234"

	const want = "vps-status-control version=v0.1.0 commit=abc1234 protocol=v1"
	if got := String("control"); got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}
