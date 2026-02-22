package attractor

import (
	"fmt"
	"sort"
	"strings"
)

func ValidateGraph(g *Graph) []Diagnostic {
	d := []Diagnostic{}
	if g == nil {
		return []Diagnostic{{Level: "ERROR", Message: "graph is nil"}}
	}
	incoming := map[string]int{}
	outgoing := map[string]int{}
	for _, e := range g.Edges {
		outgoing[e.From]++
		incoming[e.To]++
		if _, ok := g.Nodes[e.To]; !ok {
			d = append(d, Diagnostic{Level: "ERROR", Message: fmt.Sprintf("edge target missing: %s", e.To)})
		}
		c := strings.TrimSpace(e.StringAttr("condition", ""))
		if c != "" && c != "outcome=success" && c != "outcome=fail" && c != "outcome=retry" && c != "outcome=partial_success" {
			d = append(d, Diagnostic{Level: "ERROR", Message: fmt.Sprintf("unsupported condition: %s", c)})
		}
	}

	starts := []*Node{}
	exits := []*Node{}
	for _, n := range g.Nodes {
		shape := n.Shape()
		typ := n.Type()
		if shape == "Mdiamond" || n.ID == "start" {
			starts = append(starts, n)
		}
		if shape == "Msquare" || n.ID == "exit" || n.ID == "end" {
			exits = append(exits, n)
		}
		if err := validateUnsupportedHandler(shape, typ); err != nil {
			d = append(d, Diagnostic{Level: "ERROR", Message: err.Error()})
		}
		if _, err := ParseAllowedWritePaths(n); err != nil {
			d = append(d, Diagnostic{Level: "ERROR", Message: err.Error()})
		}
	}

	if len(starts) != 1 {
		d = append(d, Diagnostic{Level: "ERROR", Message: "must have exactly one start node"})
	}
	if len(exits) < 1 {
		d = append(d, Diagnostic{Level: "ERROR", Message: "must have at least one exit node"})
	}
	if len(starts) == 1 {
		if incoming[starts[0].ID] > 0 {
			d = append(d, Diagnostic{Level: "ERROR", Message: "start node cannot have incoming edges"})
		}
	}
	for _, n := range exits {
		if outgoing[n.ID] > 0 {
			d = append(d, Diagnostic{Level: "ERROR", Message: fmt.Sprintf("exit node has outgoing edges: %s", n.ID)})
		}
	}

	if len(starts) == 1 {
		seen := map[string]bool{}
		queue := []string{starts[0].ID}
		for len(queue) > 0 {
			id := queue[0]
			queue = queue[1:]
			if seen[id] {
				continue
			}
			seen[id] = true
			for _, e := range g.Edges {
				if e.From == id {
					queue = append(queue, e.To)
				}
			}
		}
		for id := range g.Nodes {
			if !seen[id] {
				d = append(d, Diagnostic{Level: "ERROR", Message: fmt.Sprintf("unreachable node: %s", id)})
			}
		}
	}

	sort.Slice(d, func(i, j int) bool {
		return d[i].Message < d[j].Message
	})
	return d
}

func validateUnsupportedHandler(shape, typ string) error {
	pairs := [][2]string{{"hexagon", "wait.human"}, {"diamond", "conditional"}, {"component", "parallel"}, {"tripleoctagon", "parallel.fan_in"}, {"house", "stack.manager_loop"}}
	for _, p := range pairs {
		if shape == p[0] || typ == p[1] {
			return fmt.Errorf("unsupported v1+ handler in v0: shape=%s type=%s", shape, typ)
		}
	}
	supportedShapes := map[string]bool{"Mdiamond": true, "Msquare": true, "box": true, "parallelogram": true, "": true}
	supportedTypes := map[string]bool{"": true, "start": true, "exit": true, "codergen": true, "tool": true}
	if !supportedShapes[shape] {
		return fmt.Errorf("unsupported shape: %s", shape)
	}
	if !supportedTypes[typ] {
		return fmt.Errorf("unsupported type: %s", typ)
	}
	return nil
}

func HasErrors(diags []Diagnostic) bool {
	for _, d := range diags {
		if d.Level == "ERROR" {
			return true
		}
	}
	return false
}
