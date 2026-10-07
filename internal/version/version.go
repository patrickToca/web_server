// Package version carries build-time metadata injected by the linker.
//
// A release build overrides the variables below with -ldflags -X:
//
//	go build -ldflags "\
//	  -X mywebapp/internal/version.Version=v0.7.0 \
//	  -X mywebapp/internal/version.GitCommit=9d4e6e335a06 \
//	  -X mywebapp/internal/version.GitBranch=main \
//	  -X mywebapp/internal/version.BuildDate=2026-10-02T12:34:56Z \
//	  -X mywebapp/internal/version.GitState=v0.7.0" \
//	  -o bin/mywebapp ./cmd/server
//
// A plain `go build` or `go run` leaves them empty. In that case Get()
// falls back to the VCS stamp embedded by the Go toolchain (available
// since Go 1.18), so a development binary still reports something
// useful: the commit hash, the build time, and whether the working
// tree was dirty.
//
// The package is deliberately dependency-free apart from the standard
// library, and free of side effects beyond reading runtime metadata.
package version

import (
	"runtime"
	"runtime/debug"
	"strings"
)

// Build-time variables. All are strings so the linker can set them
// with -X. Non-string types are not supported by -X.
var (
	// Version is the semantic version of this build, e.g. "v0.7.0".
	// For a build taken at an exact tag, the build script sets this
	// to the tag name. For any other build, it is left empty and the
	// fallback path fills it from the VCS stamp.
	Version = ""

	// GitCommit is the short commit hash (12 hex chars) the binary
	// was built from.
	GitCommit = ""

	// GitBranch is the branch name the build was taken from, e.g.
	// "main" or "release/0.7.0". Empty for detached HEAD builds
	// unless the build script fills it in.
	GitBranch = ""

	// BuildDate is the UTC timestamp of the build, RFC 3339.
	BuildDate = ""

	// GitState is the value of `git describe --tags --always --dirty`
	// at build time. Examples:
	//
	//	v0.7.0                  exact tag
	//	v0.7.0-3-gabc1234       three commits after the tag
	//	v0.7.0-3-gabc1234-dirty uncommitted changes present
	//	abc1234                 no tags reachable
	GitState = ""
)

// Info is the resolved view of the build metadata. All fields are
// safe to log and to serialise. It is what the /version endpoint
// returns and what main() logs at startup.
type Info struct {
	Version   string `json:"version"`
	GitCommit string `json:"commit"`
	GitBranch string `json:"branch"`
	GitState  string `json:"state"`
	BuildDate string `json:"built"`
	GoVersion string `json:"go"`
	Modified  bool   `json:"modified"`
}

// Get returns the resolved build information.
//
// Resolution order for each field:
//
//  1. The value injected by -ldflags -X, if non-empty.
//  2. The value from debug.ReadBuildInfo (VCS stamp), if present.
//  3. A safe placeholder.
//
// Get is safe to call concurrently and repeatedly; it does not cache,
// so a future change that mutates the package-level variables at
// runtime (for example, a test) will be reflected on the next call.
func Get() Info {
	info := Info{
		Version:   Version,
		GitCommit: GitCommit,
		GitBranch: GitBranch,
		GitState:  GitState,
		BuildDate: BuildDate,
		GoVersion: strings.TrimPrefix(runtime.Version(), "go"),
	}

	bi, ok := debug.ReadBuildInfo()
	if !ok {
		// Not built with module support, or running under an
		// environment that did not stamp VCS metadata. Fall back
		// to whatever the linker provided.
		if info.Version == "" {
			info.Version = "dev"
		}
		if info.GitCommit == "" {
			info.GitCommit = "unknown"
		}
		return info
	}

	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			if info.GitCommit == "" {
				info.GitCommit = s.Value
			}
		case "vcs.time":
			if info.BuildDate == "" {
				info.BuildDate = s.Value
			}
		case "vcs.modified":
			info.Modified = s.Value == "true"
		}
	}

	// If no version was injected, prefer the module version. For a
	// build taken inside the module's own repository it is
	// "(devel)"; for `go install module@v1.2.3` it is the module
	// version. Anything else falls back to the VCS stamp.
	if info.Version == "" {
		moduleVersion := bi.Main.Version
		switch {
		case moduleVersion != "" && moduleVersion != "(devel)":
			info.Version = moduleVersion
		case info.GitState != "":
			info.Version = info.GitState
		case info.GitCommit != "":
			info.Version = "dev+" + shortHash(info.GitCommit)
			if info.Modified {
				info.Version += "-dirty"
			}
		default:
			info.Version = "dev"
		}
	}

	// Normalise the commit hash for display. The VCS stamp carries
	// the full 40-character hash; the build script injects the
	// short form. Truncate to 12 either way.
	if len(info.GitCommit) > 12 {
		info.GitCommit = info.GitCommit[:12]
	}
	if info.GitCommit == "" {
		info.GitCommit = "unknown"
	}

	return info
}

// String renders a single-line summary suitable for a log message.
//
// Example:
//
//	version=v0.7.0 commit=9d4e6e335a06 branch=main built=2026-10-02T12:34:56Z go=1.23.0
func (i Info) String() string {
	parts := []string{"version=" + i.Version, "commit=" + i.GitCommit}
	if i.GitBranch != "" {
		parts = append(parts, "branch="+i.GitBranch)
	}
	if i.GitState != "" && i.GitState != i.Version {
		parts = append(parts, "state="+i.GitState)
	}
	if i.Modified {
		parts = append(parts, "dirty=true")
	}
	if i.BuildDate != "" {
		parts = append(parts, "built="+i.BuildDate)
	}
	if i.GoVersion != "" {
		parts = append(parts, "go="+i.GoVersion)
	}
	return strings.Join(parts, " ")
}

// shortHash returns the first 7 characters of a hash, or the whole
// string if it is already shorter.
func shortHash(h string) string {
	if len(h) > 7 {
		return h[:7]
	}
	return h
}
