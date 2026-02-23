package attractor

import "testing"

func TestCommandAllowedDirectPrefix(t *testing.T) {
	if !commandAllowed("go test ./...", []string{"go test"}) {
		t.Fatal("expected direct prefix to be allowed")
	}
}

func TestCommandAllowedWithEnvPrefix(t *testing.T) {
	if !commandAllowed(`GOCACHE="$PWD/.gocache" go test ./...`, []string{"go test"}) {
		t.Fatal("expected env-prefixed command to be allowed")
	}
}

func TestCommandAllowedWithCdWrapper(t *testing.T) {
	if !commandAllowed(`cd agent && go test ./...`, []string{"go test"}) {
		t.Fatal("expected cd-wrapper command to be allowed")
	}
}

func TestCommandAllowedWithExportWrapper(t *testing.T) {
	if !commandAllowed(`export GOCACHE="$PWD/.gocache" && go build .`, []string{"go build"}) {
		t.Fatal("expected export-wrapper command to be allowed")
	}
}

func TestCommandAllowedRejectsDisallowedBase(t *testing.T) {
	if commandAllowed(`GOCACHE="$PWD/.gocache" rm -rf /tmp/x`, []string{"go test", "go build"}) {
		t.Fatal("expected disallowed base command to be rejected")
	}
}
