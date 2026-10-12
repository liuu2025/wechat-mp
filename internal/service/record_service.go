package service

import (
	"bufio"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"wechat-mp/internal/model"
)

const (
	RecordSourceWechat = "wechat"
	RecordSourceWeb    = "web"
)

// RecordQuery 是控制台流水查询条件。
type RecordQuery struct {
	Source   string
	Status   string
	Project  string
	OpenID   string
	Page     int
	PageSize int
}

// RecordService 以按月 JSONL 文件保存取码/核销快照。
// 文件仅追加；同一流水的最新快照由 LoggedAt 决定。
type RecordService struct {
	dir            string
	retention      time.Duration
	userIdentities *UserIdentityService
	mu             sync.Mutex
}

func NewRecordService(configDir string, retentionDays int, identities ...*UserIdentityService) *RecordService {
	if retentionDays <= 0 {
		retentionDays = 90
	}
	s := &RecordService{
		dir:       filepath.Join(configDir, "records"),
		retention: time.Duration(retentionDays) * 24 * time.Hour,
	}
	if len(identities) > 0 {
		s.userIdentities = identities[0]
	}
	return s
}

func (s *RecordService) RecordCreated(record *model.CodeRecord) error {
	return s.appendSnapshot("created", record)
}

func (s *RecordService) RecordVerified(record *model.CodeRecord) error {
	return s.appendSnapshot("verified", record)
}

func (s *RecordService) appendSnapshot(event string, record *model.CodeRecord) error {
	if record == nil || strings.TrimSpace(record.RecordID) == "" {
		return nil
	}
	if s.userIdentities != nil && strings.TrimSpace(record.OpenID) != "" {
		if _, err := s.userIdentities.GetOrCreate(record.OpenID, record.CreatedAt); err != nil {
			return err
		}
	}
	status := record.Status
	if status == model.StatusUsed {
		status = model.StatusVerified
	}
	project := record.Project
	projectName := record.ProjectName
	if event == "created" && record.Source == RecordSourceWechat && project == "universal" {
		project = ""
		projectName = ""
	}
	now := time.Now()
	snapshot := model.VerificationRecordEvent{
		Event:    event,
		LoggedAt: now.UnixNano(),
		VerificationRecord: model.VerificationRecord{
			ID:             record.RecordID,
			Code:           record.Code,
			Source:         record.Source,
			Status:         status,
			OpenID:         record.OpenID,
			Project:        project,
			ProjectName:    projectName,
			OwnerAccountID: record.OwnerAccountID,
			CreatedAt:      record.CreatedAt,
			ExpiresAt:      record.ExpiresAt,
			VerifiedAt:     record.VerifiedAt,
		},
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(s.dir, now.Format("2006-01")+".jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err = f.Write(append(data, '\n')); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		s.pruneOldFilesLocked(now)
	}
	return err
}

// List 返回最新状态，管理员看全部；租户只看归属于自己的项目流水。
func (s *RecordService) List(query RecordQuery, role, accountID string) (*model.VerificationRecordList, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if query.Page < 1 {
		query.Page = 1
	}
	if query.PageSize < 1 {
		query.PageSize = 30
	}
	if query.PageSize > 100 {
		query.PageSize = 100
	}

	entries, err := os.ReadDir(s.dir)
	if os.IsNotExist(err) {
		return &model.VerificationRecordList{Items: []model.VerificationRecord{}, Page: query.Page, PageSize: query.PageSize}, nil
	}
	if err != nil {
		return nil, err
	}

	latest := make(map[string]model.VerificationRecordEvent)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		if err := s.readFile(filepath.Join(s.dir, entry.Name()), latest); err != nil {
			return nil, err
		}
	}

	now := time.Now().Unix()
	cutoff := time.Now().Add(-s.retention).Unix()
	items := make([]model.VerificationRecord, 0, len(latest))
	for _, event := range latest {
		r := event.VerificationRecord
		if r.CreatedAt < cutoff {
			continue
		}
		if r.Status == model.StatusPending && r.ExpiresAt > 0 && r.ExpiresAt <= now {
			r.Status = model.StatusExpired
		}
		if role != model.RoleAdmin && (r.OwnerAccountID == "" || r.OwnerAccountID != accountID) {
			continue
		}
		if s.userIdentities != nil && r.OpenID != "" {
			r.UserNumber, _ = s.userIdentities.Lookup(r.OpenID)
		}
		if query.Source != "" && query.Source != "all" && r.Source != query.Source {
			continue
		}
		if query.Status != "" && query.Status != "all" && r.Status != query.Status {
			continue
		}
		if query.Project != "" && query.Project != "all" && r.Project != query.Project {
			continue
		}
		if query.OpenID != "" && !recordMatchesUserQuery(r, query.OpenID) {
			continue
		}
		items = append(items, r)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt == items[j].CreatedAt {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt > items[j].CreatedAt
	})

	total := len(items)
	start := (query.Page - 1) * query.PageSize
	if start > total {
		start = total
	}
	end := start + query.PageSize
	if end > total {
		end = total
	}
	return &model.VerificationRecordList{
		Items:    items[start:end],
		Total:    total,
		Page:     query.Page,
		PageSize: query.PageSize,
	}, nil
}

func recordMatchesUserQuery(record model.VerificationRecord, query string) bool {
	query = strings.TrimSpace(query)
	if query == "" {
		return true
	}
	if strings.Contains(strings.ToLower(record.OpenID), strings.ToLower(query)) {
		return true
	}
	numberText := strings.TrimSpace(strings.TrimPrefix(query, "用户"))
	number, err := strconv.ParseInt(numberText, 10, 64)
	return err == nil && record.UserNumber == number
}

func (s *RecordService) readFile(path string, latest map[string]model.VerificationRecordEvent) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		var event model.VerificationRecordEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			// JSONL 的最后一行可能因异常断电不完整；跳过坏行，不让全部历史不可用。
			log.Printf("[Records] 跳过损坏记录 %s:%d: %v", path, line, err)
			continue
		}
		if event.ID == "" {
			continue
		}
		if old, ok := latest[event.ID]; !ok || event.LoggedAt > old.LoggedAt {
			latest[event.ID] = event
		}
	}
	return scanner.Err()
}

func (s *RecordService) pruneOldFilesLocked(now time.Time) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	cutoff := now.Add(-s.retention)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		month, err := time.Parse("2006-01", strings.TrimSuffix(entry.Name(), ".jsonl"))
		if err != nil {
			continue
		}
		monthEnd := month.AddDate(0, 1, 0)
		if monthEnd.Before(cutoff) {
			if err := os.Remove(filepath.Join(s.dir, entry.Name())); err != nil {
				log.Printf("[Records] 清理过期分片失败: %v", err)
			}
		}
	}
}
