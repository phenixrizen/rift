package version

import (
	"os"
	"runtime/debug"
	"strings"
)

// Commit can be set with -ldflags "-X github.com/phenixrizen/rift/internal/version.Commit=<sha>".
var Commit = ""

func ResolveCommit() string {
	if c := strings.TrimSpace(Commit); c != "" {
		return c
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				if c := strings.TrimSpace(setting.Value); c != "" {
					return c
				}
			}
		}
	}
	if c := strings.TrimSpace(os.Getenv("RIFT_COMMIT")); c != "" {
		return c
	}
	return "v0.0.1"
}

func ShortCommit() string {
	commit := ResolveCommit()
	if len(commit) > 12 {
		return commit[:12]
	}
	return commit
}
