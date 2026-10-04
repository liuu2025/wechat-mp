package service

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// BackupService config/ 目录定时快照（账号、激活码账本与全部运行配置的数据保险）。
// 触发时机：启动一次、每 interval 一次、由调用方在优雅退出时补拍一次；保留最近 keep 份。
type BackupService struct {
	configDir string
	backupDir string
	keep      int
	interval  time.Duration
	mu        sync.Mutex
}

func NewBackupService(configDir, backupDir string, keep int, interval time.Duration) *BackupService {
	return &BackupService{
		configDir: configDir,
		backupDir: backupDir,
		keep:      keep,
		interval:  interval,
	}
}

// SnapshotNow 立即创建一份时间戳快照，返回快照目录（同一毫秒内重复调用合并为同一份）
func (s *BackupService) SnapshotNow() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stamp := time.Now().Format("20060102-150405.000")
	dest := filepath.Join(s.backupDir, stamp)
	if err := copyDir(s.configDir, dest); err != nil {
		return "", err
	}
	s.pruneOld()
	return dest, nil
}

// pruneOld 按目录名（时间戳）字典序保留最近 keep 份
func (s *BackupService) pruneOld() {
	entries, err := os.ReadDir(s.backupDir)
	if err != nil {
		return
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	sort.Strings(dirs)
	for len(dirs) > s.keep {
		_ = os.RemoveAll(filepath.Join(s.backupDir, dirs[0]))
		dirs = dirs[1:]
	}
}

// Run 周期快照循环（ctx 取消时退出；退出前的最后一份由调用方同步补拍）
func (s *BackupService) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = s.SnapshotNow()
		}
	}
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}
