package attractor

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveAgentDefaultsToStub(t *testing.T) {
	n := &Node{ID: "a", Attrs: map[string]Value{}}
	a, err := ResolveAgent(n, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.(stubAgent); !ok {
		t.Fatalf("expected stub agent, got %T", a)
	}
}

func TestCodexOptionsResolveRelativeDirs(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "workspace")
	n := &Node{
		ID: "a",
		Attrs: map[string]Value{
			"codex.workdir":  "subdir",
			"codex.add_dirs": "foo,bar",
			"codex.sandbox":  "workspace-write",
		},
	}
	opts, err := codexOptionsFromNodeAndEnv(n, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if opts.Workdir != filepath.Join(workspace, "subdir") {
		t.Fatalf("unexpected workdir: %s", opts.Workdir)
	}
	if len(opts.AddDirs) != 2 || opts.AddDirs[0] != filepath.Join(workspace, "foo") || opts.AddDirs[1] != filepath.Join(workspace, "bar") {
		t.Fatalf("unexpected add dirs: %+v", opts.AddDirs)
	}
}

func TestCodexOptionsRejectParentSegment(t *testing.T) {
	workspace := t.TempDir()
	n := &Node{
		ID: "a",
		Attrs: map[string]Value{
			"codex.workdir": "../escape",
		},
	}
	if _, err := codexOptionsFromNodeAndEnv(n, workspace); err == nil {
		t.Fatal("expected parent segment error")
	}
}

func TestBuildCodexExecArgs(t *testing.T) {
	opts := CodexOptions{
		SandboxMode:          "workspace-write",
		ApprovalPolicy:       "never",
		Workdir:              "/tmp/work",
		AddDirs:              []string{"/tmp/one", "/tmp/two"},
		Model:                "gpt-5",
		Profile:              "default",
		ConfigOverrides:      []string{`foo.bar=1`},
		AutoApproveCommands:  []string{"git status", "go test"},
		AutoApproveConfigKey: "tools.trusted_commands",
		SkipGitRepoCheck:     true,
	}
	args, err := buildCodexExecArgs(opts, "/tmp/schema.json", "/tmp/out.json")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"-s workspace-write",
		"-a never",
		"-m gpt-5",
		"-p default",
		"-c foo.bar=1",
		"-C /tmp/work",
		"--skip-git-repo-check",
		"--add-dir /tmp/one",
		"--add-dir /tmp/two",
		"--output-schema /tmp/schema.json",
		"-o /tmp/out.json",
		"-",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %q", want, joined)
		}
	}
	if !strings.Contains(joined, `-c tools.trusted_commands=["git status","go test"]`) {
		t.Fatalf("missing auto approve override in %q", joined)
	}
}

func TestBuildCodexExecArgsAutoApproveNeedsKey(t *testing.T) {
	opts := CodexOptions{
		AutoApproveCommands: []string{"go test"},
	}
	if _, err := buildCodexExecArgs(opts, "/tmp/schema.json", "/tmp/out.json"); err == nil {
		t.Fatal("expected error when auto approve key missing")
	}
}

func TestCodexOptionsDefaultBlockedReadPaths(t *testing.T) {
	t.Setenv("ATTRACTOR_CODEX_BLOCK_READ_PATHS", "")
	workspace := t.TempDir()
	n := &Node{ID: "a", Attrs: map[string]Value{}}
	opts, err := codexOptionsFromNodeAndEnv(n, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if len(opts.BlockReadPaths) != 1 || opts.BlockReadPaths[0] != "scripts/scenarios/" {
		t.Fatalf("unexpected blocked read paths: %+v", opts.BlockReadPaths)
	}
}

func TestCodexOptionsAllowReadScenariosDisablesDefaultBlock(t *testing.T) {
	workspace := t.TempDir()
	n := &Node{
		ID: "a",
		Attrs: map[string]Value{
			"codex.allow_read_scenarios": true,
		},
	}
	opts, err := codexOptionsFromNodeAndEnv(n, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if len(opts.BlockReadPaths) != 0 {
		t.Fatalf("expected no blocked read paths, got %+v", opts.BlockReadPaths)
	}
}

func TestCodexOptionsRejectInvalidBlockedReadPath(t *testing.T) {
	workspace := t.TempDir()
	n := &Node{
		ID: "a",
		Attrs: map[string]Value{
			"codex.block_read_paths": "../secret",
		},
	}
	if _, err := codexOptionsFromNodeAndEnv(n, workspace); err == nil {
		t.Fatal("expected invalid blocked read path error")
	}
}
