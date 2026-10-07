package version

import (
	"strings"
	"testing"
)

// setInjected installs the given values into the package-level
// variables and returns a restore function.
func setInjected(version, commit, branch, date, state string) func() {
	origV := Version
	origC := GitCommit
	origB := GitBranch
	origD := BuildDate
	origS := GitState

	Version = version
	GitCommit = commit
	GitBranch = branch
	BuildDate = date
	GitState = state

	return func() {
		Version = origV
		GitCommit = origC
		GitBranch = origB
		BuildDate = origD
		GitState = origS
	}
}

func TestGet_UsesInjectedValues(t *testing.T) {
	defer setInjected("v0.7.1", "3656875f99c7", "main",
		"2026-10-02T12:34:56Z", "v0.7.1")()

	info := Get()

	if info.Version != "v0.7.1" {
		t.Errorf("Version = %q, want v0.7.1", info.Version)
	}
	if info.GitCommit != "3656875f99c7" {
		t.Errorf("GitCommit = %q", info.GitCommit)
	}
	if info.GitBranch != "main" {
		t.Errorf("GitBranch = %q", info.GitBranch)
	}
	if info.BuildDate != "2026-10-02T12:34:56Z" {
		t.Errorf("BuildDate = %q", info.BuildDate)
	}
	if info.GitState != "v0.7.1" {
		t.Errorf("GitState = %q", info.GitState)
	}
	if info.GoVersion == "" {
		t.Error("GoVersion is empty")
	}
}

func TestGet_TruncatesLongCommit(t *testing.T) {
	defer setInjected("v0.7.1",
		"3656875f99c753c7e8b94d16368e91daf88c865e",
		"main", "", "")()

	info := Get()
	if len(info.GitCommit) != 12 {
		t.Errorf("GitCommit = %q (len %d), want 12-char", info.GitCommit, len(info.GitCommit))
	}
}

func TestGet_KeepsShortCommit(t *testing.T) {
	defer setInjected("", "abc1234", "", "", "")()

	info := Get()
	if info.GitCommit != "abc1234" {
		t.Errorf("GitCommit = %q, want abc1234", info.GitCommit)
	}
}

func TestGet_NeverEmpty(t *testing.T) {
	defer setInjected("", "", "", "", "")()

	info := Get()
	if info.Version == "" {
		t.Error("Version must never be empty")
	}
	if info.GitCommit == "" {
		t.Error("GitCommit must never be empty")
	}
}

func TestString_IncludesPopulatedFields(t *testing.T) {
	defer setInjected("v0.7.1", "3656875f99c7", "main",
		"2026-10-02T12:34:56Z", "v0.7.1")()

	s := Get().String()
	for _, want := range []string{
		"version=v0.7.1",
		"commit=3656875f99c7",
		"branch=main",
		"built=2026-10-02T12:34:56Z",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("String() missing %q\n  got: %s", want, s)
		}
	}
}

func TestString_ShowsDirtyFlag(t *testing.T) {
	info := Info{Version: "v0.7.1-dirty", Modified: true}
	if !strings.Contains(info.String(), "dirty=true") {
		t.Errorf("String() should render dirty=true; got: %s", info.String())
	}
}
