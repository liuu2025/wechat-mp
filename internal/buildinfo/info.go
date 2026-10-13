// Package buildinfo exposes immutable metadata of the running binary, not the latest remote release.
package buildinfo

import (
	"regexp"
	"runtime/debug"
	"strings"
)

// These values are injected by cmd/buildmeta at compile time. Unstamped native builds are development builds.
var (
	Version   = "dev"
	Revision  = ""
	Dirty     = "unknown"
	Tagged    = "false"
	Timestamp = ""
)

var semverPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)
var descriptionPattern = regexp.MustCompile(`^(.*)-([0-9]+)-g[0-9a-f]+$`)
var revisionPattern = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

type Info struct {
	Version        string `json:"version"`
	DisplayVersion string `json:"display_version"`
	Revision       string `json:"revision,omitempty"`
	BuiltAt        string `json:"built_at,omitempty"`
	Development    bool   `json:"development"`
	Dirty          bool   `json:"dirty"`
}

func IsVersionTag(value string) bool {
	parts := semverPattern.FindStringSubmatch(value)
	if parts == nil {
		return false
	}
	for _, identifier := range strings.Split(parts[4], ".") {
		if len(identifier) > 1 && identifier[0] == '0' && strings.Trim(identifier, "0123456789") == "" {
			return false
		}
	}
	return true
}

func IsRevision(value string) bool { return revisionPattern.MatchString(value) }

func BaseTag(description string) string {
	if parts := descriptionPattern.FindStringSubmatch(description); parts != nil && IsVersionTag(parts[1]) {
		return parts[1]
	}
	if IsVersionTag(description) {
		return description
	}
	return ""
}

func FromMetadata(version, revision, dirty, timestamp string, tagged bool) Info {
	if version == "" {
		version = "dev"
	}
	if !IsRevision(revision) {
		revision = ""
	}
	info := Info{Version: version, Revision: revision, Dirty: dirty == "true", BuiltAt: timestamp}
	info.Development = !tagged || !IsVersionTag(version) || revision == "" || dirty != "false"
	info.DisplayVersion = version
	if info.Development {
		info.DisplayVersion = "开发版"
		if base := BaseTag(version); base != "" {
			if tagged && IsVersionTag(version) {
				base = version
			}
			info.DisplayVersion = base + " · 开发版"
		}
	}
	return info
}

func Current() Info {
	revision, dirty := Revision, Dirty
	// Native go build may supply a revision even without the Docker build helper.
	// Never infer an official tag from a module version or from files on the live server.
	if revision == "" {
		if info, ok := debug.ReadBuildInfo(); ok {
			for _, setting := range info.Settings {
				switch setting.Key {
				case "vcs.revision":
					revision = setting.Value
				case "vcs.modified":
					dirty = setting.Value
				}
			}
		}
	}
	return FromMetadata(Version, revision, dirty, Timestamp, Tagged == "true")
}
