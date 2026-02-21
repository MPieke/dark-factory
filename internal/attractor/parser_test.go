package attractor

import "testing"

func TestParseMinimal(t *testing.T) {
	dot := `digraph G { start [shape=Mdiamond]; a [shape=box]; exit [shape=Msquare]; start -> a; a -> exit; }`
	g, err := ParseDOT(dot)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Nodes) != 3 || len(g.Edges) != 2 {
		t.Fatalf("unexpected graph size nodes=%d edges=%d", len(g.Nodes), len(g.Edges))
	}
}

func TestParseChainedEdgesExpand(t *testing.T) {
	dot := `digraph G { start [shape=Mdiamond]; a; b; exit [shape=Msquare]; start -> a -> b -> exit [weight=2]; }`
	g, err := ParseDOT(dot)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Edges) != 3 {
		t.Fatalf("expected 3 edges got %d", len(g.Edges))
	}
	for _, e := range g.Edges {
		if e.IntAttr("weight", 0) != 2 {
			t.Fatalf("expected weight=2 got %+v", e.Attrs)
		}
	}
}

func TestParseDefaultsApply(t *testing.T) {
	dot := `digraph G {
	  node [shape=box, prompt="p"];
	  edge [weight=7];
	  start [shape=Mdiamond];
	  a;
	  exit [shape=Msquare];
	  start -> a;
	  a -> exit;
	}`
	g, err := ParseDOT(dot)
	if err != nil {
		t.Fatal(err)
	}
	if g.Nodes["a"].Shape() != "box" || g.Nodes["a"].StringAttr("prompt", "") != "p" {
		t.Fatalf("node defaults did not apply: %+v", g.Nodes["a"].Attrs)
	}
	if g.Edges[0].IntAttr("weight", 0) != 7 || g.Edges[1].IntAttr("weight", 0) != 7 {
		t.Fatalf("edge defaults did not apply")
	}
}

func TestParseUnknownAttrsPreserved(t *testing.T) {
	dot := `digraph G { start [shape=Mdiamond, x_custom="1"]; exit [shape=Msquare]; start -> exit [mystery=42]; }`
	g, err := ParseDOT(dot)
	if err != nil {
		t.Fatal(err)
	}
	if g.Nodes["start"].StringAttr("x_custom", "") != "1" {
		t.Fatalf("unknown node attr missing")
	}
	if g.Edges[0].IntAttr("mystery", 0) != 42 {
		t.Fatalf("unknown edge attr missing")
	}
}
