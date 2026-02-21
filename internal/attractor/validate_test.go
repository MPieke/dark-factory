package attractor

import "testing"

func TestValidateStarts(t *testing.T) {
	dot := `digraph G { a; exit [shape=Msquare]; a -> exit; }`
	g, _ := ParseDOT(dot)
	if !HasErrors(ValidateGraph(g)) {
		t.Fatal("expected error for missing start")
	}
	dot2 := `digraph G { start [shape=Mdiamond]; s2 [shape=Mdiamond]; exit [shape=Msquare]; start -> exit; s2 -> exit; }`
	g2, _ := ParseDOT(dot2)
	if !HasErrors(ValidateGraph(g2)) {
		t.Fatal("expected error for multiple starts")
	}
}

func TestValidateExit(t *testing.T) {
	dot := `digraph G { start [shape=Mdiamond]; a; start -> a; }`
	g, _ := ParseDOT(dot)
	if !HasErrors(ValidateGraph(g)) {
		t.Fatal("expected error for missing exit")
	}
}

func TestValidateMissingTarget(t *testing.T) {
	g := NewGraph()
	g.Nodes["start"] = &Node{ID: "start", Attrs: map[string]Value{"shape": "Mdiamond"}}
	g.Nodes["exit"] = &Node{ID: "exit", Attrs: map[string]Value{"shape": "Msquare"}}
	g.Edges = []*Edge{{From: "start", To: "ghost", Attrs: map[string]Value{}}}
	if !HasErrors(ValidateGraph(g)) {
		t.Fatal("expected missing target error")
	}
}

func TestValidateReachability(t *testing.T) {
	dot := `digraph G { start [shape=Mdiamond]; a; exit [shape=Msquare]; orphan; start -> a; a -> exit; }`
	g, _ := ParseDOT(dot)
	if !HasErrors(ValidateGraph(g)) {
		t.Fatal("expected reachability error")
	}
}

func TestValidateUnsupportedShape(t *testing.T) {
	dot := `digraph G { start [shape=Mdiamond]; h [shape=hexagon]; exit [shape=Msquare]; start -> h; h -> exit; }`
	g, _ := ParseDOT(dot)
	if !HasErrors(ValidateGraph(g)) {
		t.Fatal("expected unsupported shape error")
	}
}

func TestValidateConditionRestricted(t *testing.T) {
	dot := `digraph G { start [shape=Mdiamond]; a; exit [shape=Msquare]; start -> a [condition="context.foo=true"]; a -> exit; }`
	g, _ := ParseDOT(dot)
	if !HasErrors(ValidateGraph(g)) {
		t.Fatal("expected condition validation error")
	}
	dot2 := `digraph G { start [shape=Mdiamond]; a; exit [shape=Msquare]; start -> a [condition="outcome=success"]; a -> exit; }`
	g2, _ := ParseDOT(dot2)
	if HasErrors(ValidateGraph(g2)) {
		t.Fatal("outcome condition should be valid")
	}
}

func TestValidateAllowlistPaths(t *testing.T) {
	bad1 := `digraph G { start [shape=Mdiamond]; a [allowed_write_paths="/etc/passwd"]; exit [shape=Msquare]; start -> a; a -> exit; }`
	g1, _ := ParseDOT(bad1)
	if !HasErrors(ValidateGraph(g1)) {
		t.Fatal("expected absolute path error")
	}
	bad2 := `digraph G { start [shape=Mdiamond]; a [allowed_write_paths="../x"]; exit [shape=Msquare]; start -> a; a -> exit; }`
	g2, _ := ParseDOT(bad2)
	if !HasErrors(ValidateGraph(g2)) {
		t.Fatal("expected parent path error")
	}
	ok := `digraph G { start [shape=Mdiamond]; a [allowed_write_paths=""]; exit [shape=Msquare]; start -> a; a -> exit; }`
	g3, _ := ParseDOT(ok)
	if HasErrors(ValidateGraph(g3)) {
		t.Fatal("empty allowlist should be valid")
	}
}
