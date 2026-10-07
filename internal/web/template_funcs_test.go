package web

import (
	"html/template"
	"testing"
)

func TestGetTemplateFuncs_Arithmetic(t *testing.T) {
	f := GetTemplateFuncs()

	if f["add"] == nil || f["sub"] == nil || f["mul"] == nil || f["seq"] == nil || f["safe"] == nil {
		t.Fatal("one or more expected functions is missing")
	}

	add := f["add"].(func(int, int) int)
	sub := f["sub"].(func(int, int) int)
	mul := f["mul"].(func(int, int) int)

	if got := add(2, 3); got != 5 {
		t.Errorf("add(2,3) = %d", got)
	}
	if got := sub(10, 4); got != 6 {
		t.Errorf("sub(10,4) = %d", got)
	}
	if got := mul(3, 4); got != 12 {
		t.Errorf("mul(3,4) = %d", got)
	}
}

func TestGetTemplateFuncs_Seq(t *testing.T) {
	f := GetTemplateFuncs()
	seq := f["seq"].(func(int, int) []int)

	got := seq(1, 5)
	if len(got) != 5 || got[0] != 1 || got[4] != 5 {
		t.Errorf("seq(1,5) = %v", got)
	}

	if got := seq(5, 1); len(got) != 0 {
		t.Errorf("seq(5,1) should be empty, got %v", got)
	}
}

func TestGetTemplateFuncs_Iterate(t *testing.T) {
	f := GetTemplateFuncs()
	iterate := f["iterate"].(func(int, int) []int)

	got := iterate(1, 3)
	if len(got) != 3 {
		t.Errorf("iterate(1,3) = %v", got)
	}
	if got := iterate(5, 1); len(got) != 0 {
		t.Errorf("iterate(5,1) should be empty, got %v", got)
	}
}

func TestGetTemplateFuncs_Safe(t *testing.T) {
	f := GetTemplateFuncs()

	// The concrete type is func(string) template.HTML — that is the
	// signature declared in GetTemplateFuncs. Asserting on it directly
	// avoids a runtime panic from a mismatched assertion.
	safe, ok := f["safe"].(func(string) template.HTML)
	if !ok {
		t.Fatalf("safe is not func(string) template.HTML; got %T", f["safe"])
	}

	out := safe(`<p>hello</p>`)
	if string(out) != `<p>hello</p>` {
		t.Errorf("safe(<p>hello</p>) = %q", string(out))
	}

	// An empty input should round-trip as an empty template.HTML.
	if got := safe(""); got != "" {
		t.Errorf("safe(\"\") = %q, want \"\"", string(got))
	}
}
