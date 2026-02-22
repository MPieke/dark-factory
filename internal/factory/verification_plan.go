package attractor

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

type VerificationPlan struct {
	Files    []string `json:"files"`
	Commands []string `json:"commands"`
}

func ParseVerificationPlan(raw any) (VerificationPlan, error) {
	var plan VerificationPlan
	b, err := json.Marshal(raw)
	if err != nil {
		return plan, fmt.Errorf("invalid verification plan: %w", err)
	}
	if err := json.Unmarshal(b, &plan); err != nil {
		return plan, fmt.Errorf("invalid verification plan: %w", err)
	}
	for i, f := range plan.Files {
		clean, err := normalizeRelativePath(f)
		if err != nil {
			return plan, fmt.Errorf("invalid verification file path %q: %w", f, err)
		}
		plan.Files[i] = clean
	}
	for i, c := range plan.Commands {
		c = strings.TrimSpace(c)
		if c == "" {
			return plan, fmt.Errorf("verification command cannot be empty")
		}
		plan.Commands[i] = c
	}
	if len(plan.Commands) == 0 {
		return plan, fmt.Errorf("verification plan must contain at least one command")
	}
	return plan, nil
}

func normalizeRelativePath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", fmt.Errorf("path is empty")
	}
	if strings.HasPrefix(p, "/") || filepath.IsAbs(p) {
		return "", fmt.Errorf("absolute paths are not allowed")
	}
	if strings.Contains(p, "~") {
		return "", fmt.Errorf("~ is not allowed")
	}
	clean := filepath.ToSlash(filepath.Clean(p))
	for _, seg := range strings.Split(clean, "/") {
		if seg == ".." {
			return "", fmt.Errorf("parent path segments are not allowed")
		}
	}
	return clean, nil
}

func VerificationPlanToMap(plan VerificationPlan) map[string]any {
	return map[string]any{
		"files":    append([]string{}, plan.Files...),
		"commands": append([]string{}, plan.Commands...),
	}
}
