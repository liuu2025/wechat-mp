package buildinfo

import (
	"strings"
	"testing"
)

func TestVersionFollowsMetadataNotAnApplicationConstant(t *testing.T) {
	revision := strings.Repeat("a", 40)
	for _, version := range []string{"v1.2.3", "v3.4.5", "v10.0.0-rc.1"} {
		info := FromMetadata(version, revision, "false", "2026-10-13T10:00:00Z", true)
		if info.Version != version || info.DisplayVersion != version || info.Development || info.Revision != revision {
			t.Fatalf("新版本元数据必须直接决定显示内容: %+v", info)
		}
	}
}

func TestDevelopmentAndUnknownMetadataNeverLookLikeTaggedRelease(t *testing.T) {
	revision := strings.Repeat("b", 40)
	for _, test := range []struct{ version, revision, dirty, display string }{
		{"v3.0.0", revision, "true", "v3.0.0 · 开发版"},
		{"v3.0.0", revision, "unknown", "v3.0.0 · 开发版"},
		{"v3.0.0", "", "false", "v3.0.0 · 开发版"},
		{"v3.0.0-4-gc060b24", revision, "false", "v3.0.0 · 开发版"},
		{"v3.0.0-rc.1-2-gabcdef0", revision, "false", "v3.0.0-rc.1 · 开发版"},
		{"dev", "", "unknown", "开发版"},
	} {
		tagged := IsVersionTag(test.version) && descriptionPattern.FindStringSubmatch(test.version) == nil
		info := FromMetadata(test.version, test.revision, test.dirty, "", tagged)
		if !info.Development || info.DisplayVersion != test.display {
			t.Fatalf("不能把未发布或缺少来源的构建标为正式版: %+v", info)
		}
	}
}

func TestExactTagDistinguishedFromSameLookingGitDescription(t *testing.T) {
	version, revision := "v1.2.3-4-gabcdef0", strings.Repeat("a", 40)
	tag := FromMetadata(version, revision, "false", "", true)
	description := FromMetadata(version, revision, "false", "", false)
	if tag.Development || tag.DisplayVersion != version || !description.Development || description.DisplayVersion != "v1.2.3 · 开发版" {
		t.Fatalf("必须使用明确的精确标签标志: tag=%+v description=%+v", tag, description)
	}
}

func TestVersionTagValidation(t *testing.T) {
	for _, tag := range []string{"v0.0.0", "v1.2.3", "1.2.3", "v1.2.3-rc.1", "v1.2.3+build.123"} {
		if !IsVersionTag(tag) {
			t.Fatalf("合法语义版本被拒绝: %s", tag)
		}
	}
	for _, tag := range []string{"v01.2.3", "v1.2", "v1.2.3-01", "v1.2.3;echo", "v1.2.3 dirty", "$(command)", "$Format:%D$"} {
		if IsVersionTag(tag) {
			t.Fatalf("非法语义版本被接受: %s", tag)
		}
	}
}
