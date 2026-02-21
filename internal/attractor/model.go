package attractor

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type Value = any

type Graph struct {
	Nodes map[string]*Node
	Edges []*Edge
	Attrs map[string]Value
}

type Node struct {
	ID    string
	Attrs map[string]Value
}

type Edge struct {
	From  string
	To    string
	Attrs map[string]Value
}

type Context map[string]any

type Diagnostic struct {
	Level   string
	Message string
}

func NewGraph() *Graph {
	return &Graph{Nodes: map[string]*Node{}, Edges: []*Edge{}, Attrs: map[string]Value{}}
}

func (n *Node) StringAttr(k, def string) string {
	if n == nil {
		return def
	}
	v, ok := n.Attrs[k]
	if !ok {
		return def
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Sprintf("%v", v)
	}
	return s
}

func (n *Node) BoolAttr(k string, def bool) bool {
	if n == nil {
		return def
	}
	v, ok := n.Attrs[k]
	if !ok {
		return def
	}
	switch t := v.(type) {
	case bool:
		return t
	case string:
		b, err := strconv.ParseBool(strings.TrimSpace(t))
		if err == nil {
			return b
		}
	}
	return def
}

func (n *Node) IntAttr(k string, def int) int {
	if n == nil {
		return def
	}
	v, ok := n.Attrs[k]
	if !ok {
		return def
	}
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(t))
		if err == nil {
			return i
		}
	}
	return def
}

func (n *Node) DurationAttr(k string) (time.Duration, bool) {
	v, ok := n.Attrs[k]
	if !ok {
		return 0, false
	}
	d, ok := v.(time.Duration)
	if ok {
		return d, true
	}
	if s, ok := v.(string); ok {
		d, err := ParseDurationV0(s)
		if err == nil {
			return d, true
		}
	}
	return 0, false
}

func (n *Node) Shape() string {
	return n.StringAttr("shape", "box")
}

func (n *Node) Type() string {
	return n.StringAttr("type", "")
}

func (n *Node) Label() string {
	return n.StringAttr("label", n.ID)
}

func (e *Edge) StringAttr(k, def string) string {
	if e == nil {
		return def
	}
	v, ok := e.Attrs[k]
	if !ok {
		return def
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Sprintf("%v", v)
	}
	return s
}

func (e *Edge) IntAttr(k string, def int) int {
	if e == nil {
		return def
	}
	v, ok := e.Attrs[k]
	if !ok {
		return def
	}
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(t))
		if err == nil {
			return i
		}
	}
	return def
}

func ParseDurationV0(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return 0, fmt.Errorf("invalid duration: %q", s)
	}
	for _, unit := range []string{"ms", "s", "m", "h", "d"} {
		if strings.HasSuffix(s, unit) {
			num := strings.TrimSuffix(s, unit)
			i, err := strconv.Atoi(num)
			if err != nil {
				return 0, fmt.Errorf("invalid duration: %q", s)
			}
			switch unit {
			case "ms":
				return time.Duration(i) * time.Millisecond, nil
			case "s":
				return time.Duration(i) * time.Second, nil
			case "m":
				return time.Duration(i) * time.Minute, nil
			case "h":
				return time.Duration(i) * time.Hour, nil
			case "d":
				return time.Duration(i) * 24 * time.Hour, nil
			}
		}
	}
	return 0, fmt.Errorf("invalid duration: %q", s)
}

func ParseAllowedWritePaths(n *Node) ([]string, error) {
	raw := strings.TrimSpace(n.StringAttr("allowed_write_paths", ""))
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			return nil, fmt.Errorf("allowed_write_paths contains empty entry")
		}
		if strings.HasPrefix(p, "/") {
			return nil, fmt.Errorf("allowed_write_paths contains absolute path: %s", p)
		}
		if strings.Contains(p, "..") {
			return nil, fmt.Errorf("allowed_write_paths contains parent segment: %s", p)
		}
		out = append(out, p)
	}
	return out, nil
}
