package attractor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type AgentRequest struct {
	Prompt    string
	NodeID    string
	NodeDir   string
	Workspace string
}

type AgentResponse struct {
	Outcome            string         `json:"outcome"`
	PreferredNextLabel string         `json:"preferred_next_label"`
	SuggestedNextIDs   []string       `json:"suggested_next_ids"`
	ContextUpdates     map[string]any `json:"context_updates"`
	Notes              string         `json:"notes"`
	FailureReason      string         `json:"failure_reason"`
}

type Agent interface {
	Run(req AgentRequest) (AgentResponse, error)
}

type CodexOptions struct {
	SandboxMode          string
	ApprovalPolicy       string
	Workdir              string
	AddDirs              []string
	Model                string
	Profile              string
	ConfigOverrides      []string
	AutoApproveCommands  []string
	AutoApproveConfigKey string
	SkipGitRepoCheck     bool
	DangerousBypass      bool
}

func ResolveAgent(node *Node, workspace string) (Agent, error) {
	name := strings.TrimSpace(node.StringAttr("agent.backend", ""))
	if name == "" {
		name = strings.TrimSpace(os.Getenv("ATTRACTOR_AGENT_BACKEND"))
	}
	if name == "" {
		legacy := strings.TrimSpace(os.Getenv("ATTRACTION_BACKEND"))
		if legacy == "" {
			legacy = strings.TrimSpace(os.Getenv("ATTRACTOR_BACKEND"))
		}
		if legacy == "codex" || legacy == "stub" {
			name = legacy
		}
	}
	if name == "" {
		name = "stub"
	}
	switch name {
	case "stub":
		return stubAgent{}, nil
	case "codex":
		opts, err := codexOptionsFromNodeAndEnv(node, workspace)
		if err != nil {
			return nil, err
		}
		return codexAgent{opts: opts}, nil
	default:
		return nil, fmt.Errorf("unknown agent backend: %s", name)
	}
}

type stubAgent struct{}

func (stubAgent) Run(_ AgentRequest) (AgentResponse, error) {
	resp := "real backend not configured in v0; default success"
	return AgentResponse{
		Outcome:        "success",
		Notes:          resp,
		ContextUpdates: map[string]any{},
	}, nil
}

func codexOptionsFromNodeAndEnv(node *Node, workspace string) (CodexOptions, error) {
	opts := CodexOptions{
		SandboxMode:    pickString(node.StringAttr("codex.sandbox", ""), os.Getenv("ATTRACTOR_CODEX_SANDBOX"), "workspace-write"),
		ApprovalPolicy: pickString(node.StringAttr("codex.approval", ""), os.Getenv("ATTRACTOR_CODEX_APPROVAL"), ""),
		Workdir:        pickString(node.StringAttr("codex.workdir", ""), os.Getenv("ATTRACTOR_CODEX_WORKDIR"), ""),
		Model:          pickString(node.StringAttr("codex.model", ""), os.Getenv("ATTRACTOR_CODEX_MODEL"), ""),
		Profile:        pickString(node.StringAttr("codex.profile", ""), os.Getenv("ATTRACTOR_CODEX_PROFILE"), ""),
	}
	opts.DangerousBypass = node.BoolAttr("codex.dangerous_bypass", false) || parseBoolEnv("ATTRACTOR_CODEX_DANGEROUS_BYPASS")
	opts.SkipGitRepoCheck = node.BoolAttr("codex.skip_git_repo_check", false) || parseBoolEnv("ATTRACTOR_CODEX_SKIP_GIT_REPO_CHECK")
	opts.AddDirs = pickList(node.StringAttr("codex.add_dirs", ""), os.Getenv("ATTRACTOR_CODEX_ADD_DIRS"))
	opts.ConfigOverrides = pickConfigOverrides(node.StringAttr("codex.config_overrides", ""), os.Getenv("ATTRACTOR_CODEX_CONFIG_OVERRIDES"))
	opts.AutoApproveCommands = pickList(node.StringAttr("codex.auto_approve_commands", ""), os.Getenv("ATTRACTOR_CODEX_AUTO_APPROVE_COMMANDS"))
	opts.AutoApproveConfigKey = pickString(node.StringAttr("codex.auto_approve_config_key", ""), os.Getenv("ATTRACTOR_CODEX_AUTO_APPROVE_CONFIG_KEY"), "")

	wd, err := resolveDir(workspace, opts.Workdir)
	if err != nil {
		return CodexOptions{}, err
	}
	opts.Workdir = wd
	resolved := make([]string, 0, len(opts.AddDirs))
	for _, p := range opts.AddDirs {
		r, err := resolveDir(workspace, p)
		if err != nil {
			return CodexOptions{}, err
		}
		resolved = append(resolved, r)
	}
	opts.AddDirs = resolved
	return opts, nil
}

func pickString(primary, secondary, def string) string {
	if strings.TrimSpace(primary) != "" {
		return strings.TrimSpace(primary)
	}
	if strings.TrimSpace(secondary) != "" {
		return strings.TrimSpace(secondary)
	}
	return def
}

func pickList(primary, secondary string) []string {
	raw := secondary
	if strings.TrimSpace(primary) != "" {
		raw = primary
	}
	return splitCSV(raw)
}

func pickConfigOverrides(primary, secondary string) []string {
	raw := secondary
	if strings.TrimSpace(primary) != "" {
		raw = primary
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ";;")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseBoolEnv(key string) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	return v == "1" || v == "true" || v == "yes"
}

func resolveDir(workspace, p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return workspace, nil
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p), nil
	}
	if strings.Contains(p, "~") {
		return "", fmt.Errorf("path %q contains unsupported ~", p)
	}
	clean := filepath.Clean(p)
	for _, seg := range strings.Split(filepath.ToSlash(clean), "/") {
		if seg == ".." {
			return "", fmt.Errorf("path %q contains parent segment", p)
		}
	}
	return filepath.Join(workspace, clean), nil
}
