package attractor

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type AgentRequest struct {
	Prompt    string
	NodeID    string
	NodeDir   string
	Workspace string
	Logger    *slog.Logger
}

type AgentResponse struct {
	Outcome            string            `json:"outcome"`
	PreferredNextLabel string            `json:"preferred_next_label"`
	SuggestedNextIDs   []string          `json:"suggested_next_ids"`
	ContextUpdates     map[string]any    `json:"context_updates"`
	VerificationPlan   *VerificationPlan `json:"verification_plan,omitempty"`
	Notes              string            `json:"notes"`
	FailureReason      string            `json:"failure_reason"`
}

type Agent interface {
	Run(req AgentRequest) (AgentResponse, error)
}

type CodexOptions struct {
	SandboxMode          string
	ApprovalPolicy       string
	Workdir              string
	AddDirs              []string
	BlockReadPaths       []string
	StrictReadScope      bool
	Model                string
	Profile              string
	TimeoutSeconds       int
	HeartbeatSeconds     int
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
		TimeoutSeconds: pickInt(node.IntAttr("codex.timeout_seconds", 0), parseIntEnv("ATTRACTOR_CODEX_TIMEOUT_SECONDS"), 0),
		HeartbeatSeconds: pickInt(
			node.IntAttr("codex.heartbeat_seconds", 0),
			parseIntEnv("ATTRACTOR_CODEX_HEARTBEAT_SECONDS"),
			15,
		),
	}
	opts.DangerousBypass = node.BoolAttr("codex.dangerous_bypass", false) || parseBoolEnv("ATTRACTOR_CODEX_DANGEROUS_BYPASS")
	opts.SkipGitRepoCheck = node.BoolAttr("codex.skip_git_repo_check", false) || parseBoolEnv("ATTRACTOR_CODEX_SKIP_GIT_REPO_CHECK")
	opts.StrictReadScope = node.BoolAttr("codex.strict_read_scope", false) || parseBoolEnv("ATTRACTOR_CODEX_STRICT_READ_SCOPE")
	opts.AddDirs = pickList(node.StringAttr("codex.add_dirs", ""), os.Getenv("ATTRACTOR_CODEX_ADD_DIRS"))
	opts.ConfigOverrides = pickConfigOverrides(node.StringAttr("codex.config_overrides", ""), os.Getenv("ATTRACTOR_CODEX_CONFIG_OVERRIDES"))
	opts.AutoApproveCommands = pickList(node.StringAttr("codex.auto_approve_commands", ""), os.Getenv("ATTRACTOR_CODEX_AUTO_APPROVE_COMMANDS"))
	opts.AutoApproveConfigKey = pickString(node.StringAttr("codex.auto_approve_config_key", ""), os.Getenv("ATTRACTOR_CODEX_AUTO_APPROVE_CONFIG_KEY"), "")
	defaultBlockedReadPaths := []string{}
	if !node.BoolAttr("codex.allow_read_scenarios", false) {
		defaultBlockedReadPaths = append(defaultBlockedReadPaths, "scripts/scenarios/")
	}
	customBlockedReadPaths := pickList(node.StringAttr("codex.block_read_paths", ""), os.Getenv("ATTRACTOR_CODEX_BLOCK_READ_PATHS"))
	blockedReadPaths := append(defaultBlockedReadPaths, customBlockedReadPaths...)
	validatedBlocked, err := validateRelativePaths(blockedReadPaths)
	if err != nil {
		return CodexOptions{}, err
	}
	opts.BlockReadPaths = validatedBlocked

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

func validateRelativePaths(paths []string) ([]string, error) {
	seen := map[string]bool{}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(filepath.ToSlash(p))
		if p == "" {
			continue
		}
		if strings.HasPrefix(p, "/") {
			return nil, fmt.Errorf("path %q must be relative", p)
		}
		clean := filepath.ToSlash(filepath.Clean(p))
		for _, seg := range strings.Split(clean, "/") {
			if seg == ".." {
				return nil, fmt.Errorf("path %q contains parent segment", p)
			}
		}
		if strings.HasSuffix(p, "/") && !strings.HasSuffix(clean, "/") {
			clean += "/"
		}
		if !seen[clean] {
			seen[clean] = true
			out = append(out, clean)
		}
	}
	return out, nil
}

func pickInt(primary, secondary, def int) int {
	if primary > 0 {
		return primary
	}
	if secondary > 0 {
		return secondary
	}
	return def
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

func parseIntEnv(key string) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0
	}
	return n
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
