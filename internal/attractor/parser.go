package attractor

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var idRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func ParseDOT(input string) (*Graph, error) {
	input = stripComments(input)
	trimmed := strings.TrimSpace(input)
	if strings.Count(trimmed, "digraph") != 1 {
		return nil, fmt.Errorf("expected exactly one digraph")
	}
	if strings.Contains(trimmed, "subgraph") {
		return nil, fmt.Errorf("subgraphs are unsupported in v0")
	}
	if strings.Contains(trimmed, "--") {
		return nil, fmt.Errorf("undirected edges are unsupported")
	}
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("invalid digraph syntax")
	}
	if !strings.HasPrefix(strings.TrimSpace(trimmed[:start]), "digraph") {
		return nil, fmt.Errorf("invalid digraph syntax")
	}
	body := trimmed[start+1 : end]
	stmts := splitStatements(body)
	g := NewGraph()
	nodeDefaults := map[string]Value{}
	edgeDefaults := map[string]Value{}

	for _, stmt := range stmts {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		switch {
		case strings.HasPrefix(stmt, "graph"):
			attrs, err := parseStmtAttrs(stmt[len("graph"):])
			if err != nil {
				return nil, err
			}
			for k, v := range attrs {
				g.Attrs[k] = v
			}
		case strings.HasPrefix(stmt, "node"):
			attrs, err := parseStmtAttrs(stmt[len("node"):])
			if err != nil {
				return nil, err
			}
			for k, v := range attrs {
				nodeDefaults[k] = v
			}
		case strings.HasPrefix(stmt, "edge"):
			attrs, err := parseStmtAttrs(stmt[len("edge"):])
			if err != nil {
				return nil, err
			}
			for k, v := range attrs {
				edgeDefaults[k] = v
			}
		case strings.Contains(stmt, "->"):
			err := parseEdgeStmt(g, stmt, edgeDefaults)
			if err != nil {
				return nil, err
			}
		default:
			err := parseNodeStmt(g, stmt, nodeDefaults)
			if err != nil {
				return nil, err
			}
		}
	}
	return g, nil
}

func stripComments(in string) string {
	lines := strings.Split(in, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "//") || strings.HasPrefix(t, "#") {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func splitStatements(body string) []string {
	var stmts []string
	var cur strings.Builder
	inQuote := false
	escaped := false
	for _, r := range body {
		if r == '"' && !escaped {
			inQuote = !inQuote
		}
		if r == ';' && !inQuote {
			stmts = append(stmts, cur.String())
			cur.Reset()
			escaped = false
			continue
		}
		cur.WriteRune(r)
		escaped = (r == '\\' && !escaped)
		if r != '\\' {
			escaped = false
		}
	}
	if strings.TrimSpace(cur.String()) != "" {
		stmts = append(stmts, cur.String())
	}
	return stmts
}

func parseStmtAttrs(s string) (map[string]Value, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return map[string]Value{}, nil
	}
	open := strings.Index(s, "[")
	close := strings.LastIndex(s, "]")
	if open < 0 || close <= open {
		return nil, fmt.Errorf("expected attrs block: %s", s)
	}
	return parseAttrs(s[open+1 : close])
}

func parseNodeStmt(g *Graph, stmt string, defaults map[string]Value) error {
	id := strings.TrimSpace(stmt)
	attrs := map[string]Value{}
	if i := strings.Index(stmt, "["); i >= 0 {
		id = strings.TrimSpace(stmt[:i])
		j := strings.LastIndex(stmt, "]")
		if j <= i {
			return fmt.Errorf("invalid node attrs: %s", stmt)
		}
		parsed, err := parseAttrs(stmt[i+1 : j])
		if err != nil {
			return err
		}
		attrs = parsed
	}
	if !idRe.MatchString(id) {
		return fmt.Errorf("invalid node id: %s", id)
	}
	n := g.Nodes[id]
	if n == nil {
		n = &Node{ID: id, Attrs: map[string]Value{}}
		for k, v := range defaults {
			n.Attrs[k] = v
		}
		g.Nodes[id] = n
	}
	for k, v := range attrs {
		n.Attrs[k] = v
	}
	return nil
}

func parseEdgeStmt(g *Graph, stmt string, defaults map[string]Value) error {
	lhs := stmt
	attrs := map[string]Value{}
	if i := strings.Index(stmt, "["); i >= 0 {
		lhs = strings.TrimSpace(stmt[:i])
		j := strings.LastIndex(stmt, "]")
		if j <= i {
			return fmt.Errorf("invalid edge attrs: %s", stmt)
		}
		parsed, err := parseAttrs(stmt[i+1 : j])
		if err != nil {
			return err
		}
		attrs = parsed
	}
	parts := strings.Split(lhs, "->")
	ids := make([]string, 0, len(parts))
	for _, p := range parts {
		id := strings.TrimSpace(p)
		if !idRe.MatchString(id) {
			return fmt.Errorf("invalid edge endpoint: %s", id)
		}
		ids = append(ids, id)
	}
	for i := 0; i < len(ids)-1; i++ {
		eAttrs := map[string]Value{}
		for k, v := range defaults {
			eAttrs[k] = v
		}
		for k, v := range attrs {
			eAttrs[k] = v
		}
		g.Edges = append(g.Edges, &Edge{From: ids[i], To: ids[i+1], Attrs: eAttrs})
	}
	return nil
}

func parseAttrs(body string) (map[string]Value, error) {
	pairs := splitCommaAware(body)
	out := map[string]Value{}
	for _, p := range pairs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		kv := strings.SplitN(p, "=", 2)
		if len(kv) != 2 {
			return nil, fmt.Errorf("invalid attr: %s", p)
		}
		k := strings.TrimSpace(kv[0])
		if strings.HasPrefix(k, "\"") && strings.HasSuffix(k, "\"") {
			u, err := strconv.Unquote(k)
			if err != nil {
				return nil, err
			}
			k = u
		}
		vRaw := strings.TrimSpace(kv[1])
		v, err := parseValue(vRaw)
		if err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, nil
}

func splitCommaAware(s string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	esc := false
	for _, r := range s {
		if r == '"' && !esc {
			inQuote = !inQuote
		}
		if r == ',' && !inQuote {
			out = append(out, cur.String())
			cur.Reset()
			esc = false
			continue
		}
		cur.WriteRune(r)
		esc = (r == '\\' && !esc)
		if r != '\\' {
			esc = false
		}
	}
	if strings.TrimSpace(cur.String()) != "" {
		out = append(out, cur.String())
	}
	return out
}

func parseValue(v string) (Value, error) {
	if strings.HasPrefix(v, "\"") && strings.HasSuffix(v, "\"") {
		u, err := strconv.Unquote(v)
		if err != nil {
			return nil, err
		}
		return u, nil
	}
	if v == "true" || v == "false" {
		b, _ := strconv.ParseBool(v)
		return b, nil
	}
	if d, err := ParseDurationV0(v); err == nil {
		return d, nil
	}
	if i, err := strconv.Atoi(v); err == nil {
		return i, nil
	}
	if f, err := strconv.ParseFloat(v, 64); err == nil {
		return f, nil
	}
	return v, nil
}
