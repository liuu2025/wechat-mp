// Package buildmeta obtains version metadata before compilation; it is never run by the live service.
package buildmeta

import (
	"cmp"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"wechat-mp/internal/buildinfo"
)

type Metadata struct {
	Version   string
	Revision  string
	Dirty     string
	Tagged    bool
	Timestamp string
}

type gitRunner func(...string) (string, error)

func Detect(root string) Metadata {
	run := func(args ...string) (string, error) {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		output, err := cmd.Output()
		return strings.TrimSpace(string(output)), err
	}
	archive, _ := os.ReadFile(filepath.Join(root, "build", "archive-version.txt"))
	return detect(root, run, string(archive), time.Now().UTC().Format(time.RFC3339))
}

func detect(root string, run gitRunner, archive, timestamp string) Metadata {
	// Check the root so an extracted archive inside another checkout cannot inherit the parent's version.
	top, rootErr := run("rev-parse", "--show-toplevel")
	revision, revisionErr := run("rev-parse", "HEAD")
	if rootErr == nil && revisionErr == nil && sameRoot(root, top) && buildinfo.IsRevision(revision) {
		meta := Metadata{Version: "dev", Revision: revision, Dirty: "unknown", Timestamp: timestamp}
		if status, err := run("status", "--porcelain", "--untracked-files=normal"); err == nil {
			meta.Dirty = "false"
			if status != "" {
				meta.Dirty = "true"
			}
		}
		// Both annotated and lightweight tags are supported; never create or change tags here.
		if tags, err := run("tag", "--points-at", "HEAD", "--sort=-version:refname"); err == nil {
			if tag := latestTag(strings.Split(tags, "\n")); tag != "" {
				meta.Version, meta.Tagged = tag, true
				return meta
			}
		}
		if description, err := run("describe", "--tags", "--long", "--abbrev=7", "--match", "v[0-9]*", "HEAD"); err == nil && buildinfo.BaseTag(description) != "" {
			meta.Version = description
		}
		return meta
	}
	return fromArchive(archive, timestamp)
}

func fromArchive(data, timestamp string) Metadata {
	meta := Metadata{Version: "dev", Dirty: "unknown", Timestamp: timestamp}
	fields := make(map[string]string)
	for _, line := range strings.Split(data, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok {
			fields[key] = strings.TrimSpace(value)
		}
	}
	if !buildinfo.IsRevision(fields["commit"]) {
		return meta // Raw $Format placeholders or incomplete metadata must not look like an official release.
	}
	meta.Revision, meta.Dirty = fields["commit"], "false"
	// %D contains only decorations at this exact commit, not tags at ancestor commits.
	var tags []string
	for _, ref := range strings.Split(fields["refs"], ",") {
		ref = strings.TrimSpace(ref)
		if !strings.HasPrefix(ref, "tag: ") {
			continue
		}
		tags = append(tags, strings.TrimPrefix(ref, "tag: "))
	}
	if tag := latestTag(tags); tag != "" {
		meta.Version, meta.Tagged = tag, true
	}
	return meta
}

// 多个标签指向同一提交时，仓库和归档使用相同的语义版本顺序，不依赖 %D 的装饰顺序。
func latestTag(tags []string) string {
	latest := ""
	for _, tag := range tags {
		if buildinfo.IsVersionTag(tag) && (latest == "" || compareTags(tag, latest) > 0) {
			latest = tag
		}
	}
	return latest
}

func compareTags(a, b string) int {
	parse := func(tag string) ([]string, string) {
		core, _, _ := strings.Cut(strings.TrimPrefix(tag, "v"), "+")
		core, pre, _ := strings.Cut(core, "-")
		return strings.Split(core, "."), pre
	}
	left, leftPre := parse(a)
	right, rightPre := parse(b)
	compareNumeric := func(x, y string) int {
		if order := cmp.Compare(len(x), len(y)); order != 0 {
			return order
		}
		return strings.Compare(x, y)
	}
	for i := range left {
		if order := compareNumeric(left[i], right[i]); order != 0 {
			return order
		}
	}
	if leftPre != rightPre {
		if leftPre == "" {
			return 1
		}
		if rightPre == "" {
			return -1
		}
		leftIDs, rightIDs := strings.Split(leftPre, "."), strings.Split(rightPre, ".")
		for i := 0; i < min(len(leftIDs), len(rightIDs)); i++ {
			x, y := leftIDs[i], rightIDs[i]
			if x == y {
				continue
			}
			xNumeric, yNumeric := strings.Trim(x, "0123456789") == "", strings.Trim(y, "0123456789") == ""
			if xNumeric && yNumeric {
				return compareNumeric(x, y)
			}
			if xNumeric != yNumeric {
				if xNumeric {
					return -1
				}
				return 1
			}
			return strings.Compare(x, y)
		}
		return cmp.Compare(len(leftIDs), len(rightIDs))
	}
	return strings.Compare(a, b) // 等价版本的 build metadata 用确定性顺序打破平局。
}

func sameRoot(a, b string) bool {
	a, errA := filepath.Abs(a)
	b, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(a); err == nil {
		a = resolved
	}
	if resolved, err := filepath.EvalSymlinks(b); err == nil {
		b = resolved
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// Values are either validated Git metadata or UTC timestamps, with no spaces/shell control characters.
func (m Metadata) LinkerFlags() string {
	return "-X wechat-mp/internal/buildinfo.Version=" + m.Version +
		" -X wechat-mp/internal/buildinfo.Revision=" + m.Revision +
		" -X wechat-mp/internal/buildinfo.Dirty=" + m.Dirty +
		" -X wechat-mp/internal/buildinfo.Tagged=" + strconv.FormatBool(m.Tagged) +
		" -X wechat-mp/internal/buildinfo.Timestamp=" + m.Timestamp
}
