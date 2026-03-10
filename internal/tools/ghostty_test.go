package tools

import "testing"

func TestAgentColor_Deterministic(t *testing.T) {
	c1 := agentColor("research")
	c2 := agentColor("research")
	if c1 != c2 {
		t.Errorf("same name produced different colors: %s vs %s", c1, c2)
	}
}

func TestAgentColor_DifferentNames(t *testing.T) {
	c1 := agentColor("research")
	c2 := agentColor("code")
	if c1 == c2 {
		t.Errorf("different names produced same color: %s", c1)
	}
}

func TestAgentColor_ValidHex(t *testing.T) {
	c := agentColor("test")
	if len(c) != 7 || c[0] != '#' {
		t.Errorf("expected #RRGGBB format, got: %s", c)
	}
}

func TestHslToRGB(t *testing.T) {
	r, g, b := hslToRGB(0, 1.0, 0.5)
	if r != 255 || g != 0 || b != 0 {
		t.Errorf("expected (255,0,0), got (%d,%d,%d)", r, g, b)
	}
	r, g, b = hslToRGB(120, 1.0, 0.5)
	if r != 0 || g != 255 || b != 0 {
		t.Errorf("expected (0,255,0), got (%d,%d,%d)", r, g, b)
	}
}
