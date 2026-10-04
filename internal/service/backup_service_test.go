package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBackup_快照内容与保留份数(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	backupDir := filepath.Join(dir, "backups")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "projects.json"), []byte(`[]`), 0o644); err != nil {
		t.Fatal(err)
	}

	bs := NewBackupService(configDir, backupDir, 2, time.Hour)
	for i := 0; i < 3; i++ {
		// 每轮修改内容，验证"最新快照包含最新数据"
		if err := os.WriteFile(filepath.Join(configDir, "projects.json"), []byte("v"+string(rune('0'+i))), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := bs.SnapshotNow(); err != nil {
			t.Fatalf("第 %d 次快照失败: %v", i+1, err)
		}
		time.Sleep(5 * time.Millisecond) // 时间戳区分
	}

	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("应只保留最近 2 份，实际 %d 份", len(entries))
	}

	// 最新一份应包含最新内容
	latest := filepath.Join(backupDir, entries[len(entries)-1].Name(), "projects.json")
	data, err := os.ReadFile(latest)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "v2" {
		t.Fatalf("最新快照应包含最新内容 v2，实际 %q", string(data))
	}
}

func TestBackup_源目录损坏时报错不崩溃(t *testing.T) {
	// 源目录不存在：SnapshotNow 应返回错误而非 panic
	bs := NewBackupService(filepath.Join(t.TempDir(), "不存在"), t.TempDir(), 2, time.Hour)
	if _, err := bs.SnapshotNow(); err == nil {
		t.Fatalf("源目录不存在应返回错误")
	}
}
