package buildmeta

import (
	"errors"
	"strings"
	"testing"

	"wechat-mp/internal/buildinfo"
)

func fakeGit(root, tags, status, describe string) gitRunner {
	return func(args ...string) (string, error) {
		switch strings.Join(args, " ") {
		case "rev-parse --show-toplevel":
			return root, nil
		case "rev-parse HEAD":
			return strings.Repeat("a", 40), nil
		case "status --porcelain --untracked-files=normal":
			return status, nil
		case "tag --points-at HEAD --sort=-version:refname":
			return tags, nil
		case "describe --tags --long --abbrev=7 --match v[0-9]* HEAD":
			return describe, nil
		default:
			return "", errors.New("unexpected Git command")
		}
	}
}

func TestDetectExactTagAndDirtyTree(t *testing.T) {
	root := t.TempDir()
	for _, version := range []string{"v1.2.3", "v2.0.0"} {
		meta := detect(root, fakeGit(root, version, "", ""), "", "2026-10-13T10:00:00Z")
		if meta.Version != version || meta.Dirty != "false" || buildinfo.FromMetadata(meta.Version, meta.Revision, meta.Dirty, meta.Timestamp, meta.Tagged).Development {
			t.Fatalf("精确标签应成为相应版本: %+v", meta)
		}
	}
	meta := detect(root, fakeGit(root, "v2.0.0", " M web/console.html", ""), "", "")
	if meta.Dirty != "true" || !buildinfo.FromMetadata(meta.Version, meta.Revision, meta.Dirty, "", meta.Tagged).Development {
		t.Fatalf("脏工作树必须标注开发版: %+v", meta)
	}
}

func TestDetectCommitsAfterTagAndNoTags(t *testing.T) {
	root := t.TempDir()
	meta := detect(root, fakeGit(root, "", "", "v3.0.0-4-gc060b24"), "", "")
	if meta.Version != "v3.0.0-4-gc060b24" || !buildinfo.FromMetadata(meta.Version, meta.Revision, meta.Dirty, "", meta.Tagged).Development {
		t.Fatalf("标签后的提交应保留具体描述且标开发版: %+v", meta)
	}
	meta = detect(root, fakeGit(root, "", "", ""), "", "")
	if meta.Version != "dev" || meta.Revision == "" {
		t.Fatalf("无标签也应保留提交号: %+v", meta)
	}
}

func TestGitArchiveMetadataAndMissingMetadata(t *testing.T) {
	revision := strings.Repeat("c", 40)
	noGit := func(...string) (string, error) { return "", errors.New("no git") }
	for _, version := range []string{"v1.2.3", "v2.0.0"} {
		meta := detect(t.TempDir(), noGit, "commit="+revision+"\nrefs=HEAD -> main, tag: "+version+"\n", "")
		if meta.Version != version || meta.Revision != revision || meta.Dirty != "false" {
			t.Fatalf("归档应保留对应版本: %+v", meta)
		}
	}
	for _, archive := range []string{"", "commit=$Format:%H$\nrefs=$Format:%D$", "commit=not-a-commit\nrefs=tag: v99.0.0"} {
		meta := detect(t.TempDir(), noGit, archive, "")
		if meta.Version != "dev" || meta.Revision != "" || meta.Dirty != "unknown" {
			t.Fatalf("缺少元数据不能猜测版本: %+v", meta)
		}
	}
	meta := fromArchive("commit="+revision+"\nrefs=HEAD -> main", "")
	if meta.Version != "dev" || meta.Revision != revision {
		t.Fatalf("无版本标签的归档应是开发版: %+v", meta)
	}
}

func TestArchiveInsideAnotherCheckoutDoesNotInheritParentVersion(t *testing.T) {
	root, parent := t.TempDir(), t.TempDir()
	meta := detect(root, fakeGit(parent, "v9.9.9", "", ""), "commit="+strings.Repeat("d", 40)+"\nrefs=tag: v1.2.3", "")
	if meta.Version != "v1.2.3" {
		t.Fatalf("不得读取归档外部父目录的版本: %+v", meta)
	}
}

func TestGitStatusFailureCannotProduceOfficialBuild(t *testing.T) {
	root := t.TempDir()
	base := fakeGit(root, "v1.2.3", "", "")
	run := func(args ...string) (string, error) {
		if args[0] == "status" {
			return "", errors.New("status failed")
		}
		return base(args...)
	}
	meta := detect(root, run, "", "")
	if meta.Dirty != "unknown" || !buildinfo.FromMetadata(meta.Version, meta.Revision, meta.Dirty, "", meta.Tagged).Development {
		t.Fatalf("未知源码状态必须降级为开发版: %+v", meta)
	}
}

func TestMultipleTagsSelectSameNewestVersionInCheckoutAndArchive(t *testing.T) {
	for _, pair := range [][]string{
		{"v3.9.0", "v3.10.0"},
		{"v3.10.0-rc.1", "v3.10.0"},
		{"v3.10.0-beta.2", "v3.10.0-beta.11"},
		{"v3.10.0-1", "v3.10.0-alpha"},
	} {
		root := t.TempDir()
		git := detect(root, fakeGit(root, pair[0]+"\n"+pair[1], "", ""), "", "")
		archive := fromArchive("commit="+strings.Repeat("a", 40)+"\nrefs=tag: "+pair[0]+", tag: "+pair[1], "")
		if git.Version != pair[1] || archive.Version != git.Version || !archive.Tagged {
			t.Fatalf("多标签版本选择不一致: git=%+v archive=%+v", git, archive)
		}
	}
}
