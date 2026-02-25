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
	if commandAllowed(`cd agent && go test ./...`, []string{"go test"}) {
		t.Fatal("expected shell wrapper command to be rejected")
	}
}

func TestCommandAllowedWithExportWrapper(t *testing.T) {
	if commandAllowed(`export GOCACHE="$PWD/.gocache" && go build .`, []string{"go build"}) {
		t.Fatal("expected export-wrapper command to be rejected")
	}
}

func TestCommandAllowedRejectsDisallowedBase(t *testing.T) {
	if commandAllowed(`GOCACHE="$PWD/.gocache" rm -rf /tmp/x`, []string{"go test", "go build"}) {
		t.Fatal("expected disallowed base command to be rejected")
	}
}

func TestParseVerificationCommandWithEnvExpansion(t *testing.T) {
	got, err := parseVerificationCommand(`GOCACHE="$PWD/.gocache" go test ./...`, "/tmp/work/agent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "go" {
		t.Fatalf("unexpected name: %q", got.Name)
	}
	if len(got.Args) != 2 || got.Args[0] != "test" || got.Args[1] != "./..." {
		t.Fatalf("unexpected args: %#v", got.Args)
	}
	if len(got.Env) != 1 || got.Env[0] != "GOCACHE=/tmp/work/agent/.gocache" {
		t.Fatalf("unexpected env: %#v", got.Env)
	}
}

func TestParseVerificationCommandRejectsUnsafeShellSyntax(t *testing.T) {
	if _, err := parseVerificationCommand(`go test ./...; rm -rf /tmp/x`, "/tmp/work/agent"); err == nil {
		t.Fatal("expected unsafe shell syntax to be rejected")
	}
}
