package service

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"wechat-mp/internal/model"
)

const firstUserNumber int64 = 1001

type storedUserIdentity struct {
	Number      int64 `json:"number"`
	FirstSeenAt int64 `json:"first_seen_at"`
}

type storedUserIdentityRegistry struct {
	Version    int                           `json:"version"`
	NextNumber int64                         `json:"next_number"`
	Users      map[string]storedUserIdentity `json:"users"`
}

// UserIdentityService 为每个曾被系统确认的 OpenID 分配永久且只增不减的用户编号。
// 映射独立于 90 天流水保存；本服务刻意不提供删除、回收或重排方法。
type UserIdentityService struct {
	path     string
	mu       sync.Mutex
	registry storedUserIdentityRegistry
}

// NewUserIdentityService 加载永久编号表，并从现有账号与流水中按最早出现时间补齐历史用户。
// 编号表损坏时直接返回错误，绝不静默重建导致编号错乱。
func NewUserIdentityService(configDir string) (*UserIdentityService, error) {
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return nil, err
	}
	s := &UserIdentityService{
		path: filepath.Join(configDir, "user_identities.json"),
		registry: storedUserIdentityRegistry{
			Version:    1,
			NextNumber: firstUserNumber,
			Users:      make(map[string]storedUserIdentity),
		},
	}

	fileExists := true
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		fileExists = false
	} else if err != nil {
		return nil, fmt.Errorf("读取用户编号表失败: %w", err)
	} else if err := json.Unmarshal(data, &s.registry); err != nil {
		return nil, fmt.Errorf("用户编号表损坏，已停止启动以避免编号重排: %w", err)
	}

	changed, err := s.validateRegistry()
	if err != nil {
		return nil, err
	}
	backfilled, err := s.backfillExistingOpenIDs(configDir)
	if err != nil {
		return nil, err
	}
	if !fileExists || changed || backfilled {
		if err := writeReplyJSON(s.path, s.registry); err != nil {
			return nil, fmt.Errorf("保存用户编号表失败: %w", err)
		}
	}
	return s, nil
}

func (s *UserIdentityService) validateRegistry() (bool, error) {
	changed := false
	if s.registry.Version == 0 {
		s.registry.Version = 1
		changed = true
	}
	if s.registry.Version != 1 {
		return false, fmt.Errorf("不支持的用户编号表版本: %d", s.registry.Version)
	}
	if s.registry.Users == nil {
		s.registry.Users = make(map[string]storedUserIdentity)
		changed = true
	}

	maxNumber := firstUserNumber - 1
	usedNumbers := make(map[int64]string, len(s.registry.Users))
	for openID, identity := range s.registry.Users {
		if strings.TrimSpace(openID) == "" || strings.TrimSpace(openID) != openID {
			return false, fmt.Errorf("用户编号表包含非法 OpenID 键")
		}
		if identity.Number < firstUserNumber {
			return false, fmt.Errorf("OpenID 对应的用户编号非法: %d", identity.Number)
		}
		if other, ok := usedNumbers[identity.Number]; ok && other != openID {
			return false, fmt.Errorf("用户编号 %d 被多个 OpenID 重复占用", identity.Number)
		}
		usedNumbers[identity.Number] = openID
		if identity.Number > maxNumber {
			maxNumber = identity.Number
		}
	}
	if s.registry.NextNumber <= maxNumber {
		s.registry.NextNumber = maxNumber + 1
		changed = true
	}
	if s.registry.NextNumber < firstUserNumber {
		s.registry.NextNumber = firstUserNumber
		changed = true
	}
	return changed, nil
}

type historicalOpenID struct {
	OpenID  string
	SeenAt  int64
	TieHint int64
}

func (s *UserIdentityService) backfillExistingOpenIDs(configDir string) (bool, error) {
	firstSeen := make(map[string]historicalOpenID)
	remember := func(openID string, seenAt, tieHint int64) {
		openID = strings.TrimSpace(openID)
		if openID == "" {
			return
		}
		candidate := historicalOpenID{OpenID: openID, SeenAt: seenAt, TieHint: tieHint}
		old, ok := firstSeen[openID]
		if !ok || candidate.SeenAt < old.SeenAt || (candidate.SeenAt == old.SeenAt && candidate.TieHint < old.TieHint) {
			firstSeen[openID] = candidate
		}
	}

	accountsPath := filepath.Join(configDir, "accounts.json")
	if data, err := os.ReadFile(accountsPath); err == nil {
		var accounts []model.Account
		if err := json.Unmarshal(data, &accounts); err != nil {
			return false, fmt.Errorf("读取历史账号以回填用户编号失败: %w", err)
		}
		for i, account := range accounts {
			seenAt := account.CreatedAt
			if seenAt <= 0 {
				seenAt = time.Now().Unix()
			}
			remember(account.OpenID, seenAt, seenAt*int64(time.Second)+int64(i))
		}
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("读取历史账号以回填用户编号失败: %w", err)
	}

	recordsDir := filepath.Join(configDir, "records")
	entries, err := os.ReadDir(recordsDir)
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("读取历史流水以回填用户编号失败: %w", err)
	}
	if err == nil {
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		var lineOrder int64
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
				continue
			}
			path := filepath.Join(recordsDir, entry.Name())
			f, openErr := os.Open(path)
			if openErr != nil {
				return false, openErr
			}
			scanner := bufio.NewScanner(f)
			scanner.Buffer(make([]byte, 64*1024), 1024*1024)
			line := 0
			for scanner.Scan() {
				line++
				lineOrder++
				var event model.VerificationRecordEvent
				if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
					log.Printf("[UserIdentity] 回填时跳过损坏记录 %s:%d: %v", path, line, err)
					continue
				}
				seenAt := event.LoggedAt / int64(time.Second)
				if seenAt <= 0 {
					seenAt = event.CreatedAt
				}
				if seenAt <= 0 {
					seenAt = time.Now().Unix()
				}
				tieHint := event.LoggedAt
				if tieHint <= 0 {
					tieHint = seenAt*int64(time.Second) + lineOrder
				}
				remember(event.OpenID, seenAt, tieHint)
			}
			scanErr := scanner.Err()
			closeErr := f.Close()
			if scanErr != nil {
				return false, scanErr
			}
			if closeErr != nil {
				return false, closeErr
			}
		}
	}

	ordered := make([]historicalOpenID, 0, len(firstSeen))
	for _, item := range firstSeen {
		if _, exists := s.registry.Users[item.OpenID]; !exists {
			ordered = append(ordered, item)
		}
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].SeenAt != ordered[j].SeenAt {
			return ordered[i].SeenAt < ordered[j].SeenAt
		}
		if ordered[i].TieHint != ordered[j].TieHint {
			return ordered[i].TieHint < ordered[j].TieHint
		}
		return ordered[i].OpenID < ordered[j].OpenID
	})
	for _, item := range ordered {
		s.registry.Users[item.OpenID] = storedUserIdentity{Number: s.registry.NextNumber, FirstSeenAt: item.SeenAt}
		s.registry.NextNumber++
	}
	return len(ordered) > 0, nil
}

// GetOrCreate 返回固定编号；新 OpenID 只有在完整编号表成功落盘后才算分配成功。
func (s *UserIdentityService) GetOrCreate(openID string, firstSeenAt int64) (int64, error) {
	openID = strings.TrimSpace(openID)
	if openID == "" {
		return 0, fmt.Errorf("openid 不能为空")
	}
	if firstSeenAt <= 0 {
		firstSeenAt = time.Now().Unix()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if identity, ok := s.registry.Users[openID]; ok {
		return identity.Number, nil
	}

	previous := s.registry
	previous.Users = make(map[string]storedUserIdentity, len(s.registry.Users))
	for key, value := range s.registry.Users {
		previous.Users[key] = value
	}
	if len(previous.Users) > 0 {
		if err := writeReplyJSON(s.path+".bak", previous); err != nil {
			return 0, fmt.Errorf("备份用户编号表失败: %w", err)
		}
	}

	number := s.registry.NextNumber
	s.registry.Users[openID] = storedUserIdentity{Number: number, FirstSeenAt: firstSeenAt}
	s.registry.NextNumber++
	if err := writeReplyJSON(s.path, s.registry); err != nil {
		s.registry = previous
		return 0, fmt.Errorf("保存用户编号失败: %w", err)
	}
	return number, nil
}

func (s *UserIdentityService) Lookup(openID string) (int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	identity, ok := s.registry.Users[strings.TrimSpace(openID)]
	return identity.Number, ok
}

type UserIdentityQuery struct {
	Keyword      string
	Registration string // registered | unregistered | all
	Kind         string // mock | ordinary | all
	Page         int
	PageSize     int
}

// List 只读永久编号表并关联账号快照；不创建编号，不改变 next_number 或文件。
func (s *UserIdentityService) List(query UserIdentityQuery, accounts []model.Account) *model.RegisteredUserList {
	s.mu.Lock()
	defer s.mu.Unlock()
	if query.PageSize < 1 {
		query.PageSize = 20
	}
	if query.PageSize > 100 {
		query.PageSize = 100
	}
	accountByOpenID := make(map[string]model.Account, len(accounts))
	for _, account := range accounts {
		accountByOpenID[account.OpenID] = account
	}
	result := &model.RegisteredUserList{
		KnownCount: len(s.registry.Users), NextNumber: s.registry.NextNumber, PageSize: query.PageSize,
	}
	items := make([]model.RegisteredUser, 0, len(s.registry.Users))
	for openID, identity := range s.registry.Users {
		item := model.RegisteredUser{OpenID: openID, UserNumber: identity.Number, FirstSeenAt: identity.FirstSeenAt, Kind: "ordinary"}
		if strings.HasPrefix(openID, "mock_wx_user_") {
			item.Kind = "mock"
			result.MockCount++
		}
		if account, ok := accountByOpenID[openID]; ok {
			item.Registered, item.Role, item.Status = true, account.Role, account.Status
			result.AccountCount++
		}
		if query.Registration == "registered" && !item.Registered || query.Registration == "unregistered" && item.Registered {
			continue
		}
		if query.Kind != "" && query.Kind != "all" && query.Kind != item.Kind {
			continue
		}
		if !recordMatchesUserQuery(model.VerificationRecord{OpenID: openID, UserNumber: identity.Number}, query.Keyword) {
			continue
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].UserNumber < items[j].UserNumber })
	result.Total = len(items)
	result.Page = boundedPage(query.Page, result.Total, query.PageSize)
	start := (result.Page - 1) * query.PageSize
	end := min(start+query.PageSize, result.Total)
	result.Items = items[start:end]
	return result
}

// 先限制页码再计算偏移，避免极大查询值乘法溢出。
func boundedPage(page, total, pageSize int) int {
	last := max(1, (total+pageSize-1)/pageSize)
	return min(max(1, page), last)
}
