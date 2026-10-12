package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"wechat-mp/internal/config"
	"wechat-mp/internal/model"
)

func TestUserIdentityPermanentAndPersistent(t *testing.T) {
	dir := t.TempDir()
	svc, err := NewUserIdentityService(dir)
	if err != nil {
		t.Fatal(err)
	}

	first, err := svc.GetOrCreate("openid_first", 100)
	if err != nil {
		t.Fatal(err)
	}
	again, err := svc.GetOrCreate("openid_first", 200)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.GetOrCreate("openid_second", 300)
	if err != nil {
		t.Fatal(err)
	}
	if first != 1001 || again != first || second != 1002 {
		t.Fatalf("编号应从 1001 递增且同一 OpenID 永久复用: first=%d again=%d second=%d", first, again, second)
	}

	reloaded, err := NewUserIdentityService(dir)
	if err != nil {
		t.Fatal(err)
	}
	if number, ok := reloaded.Lookup("openid_first"); !ok || number != 1001 {
		t.Fatalf("重启后编号丢失或变化: number=%d ok=%v", number, ok)
	}
	third, err := reloaded.GetOrCreate("openid_third", 400)
	if err != nil {
		t.Fatal(err)
	}
	if third != 1003 {
		t.Fatalf("重启后应从历史最大编号继续递增，实际 %d", third)
	}
}

func TestUserIdentityBackfillsAccountsAndRecordsByFirstSeen(t *testing.T) {
	dir := t.TempDir()
	accounts := []model.Account{{ID: "acc_later", OpenID: "openid_account", CreatedAt: 200}}
	accountData, _ := json.Marshal(accounts)
	if err := os.WriteFile(filepath.Join(dir, "accounts.json"), accountData, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "records"), 0o755); err != nil {
		t.Fatal(err)
	}
	event := model.VerificationRecordEvent{
		Event:    "created",
		LoggedAt: int64(100 * time.Second),
		VerificationRecord: model.VerificationRecord{
			ID: "rec_first", OpenID: "openid_record", CreatedAt: 100,
		},
	}
	eventData, _ := json.Marshal(event)
	if err := os.WriteFile(filepath.Join(dir, "records", "2026-01.jsonl"), append(eventData, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	svc, err := NewUserIdentityService(dir)
	if err != nil {
		t.Fatal(err)
	}
	if number, ok := svc.Lookup("openid_record"); !ok || number != 1001 {
		t.Fatalf("更早出现在流水中的用户应为用户1001: number=%d ok=%v", number, ok)
	}
	if number, ok := svc.Lookup("openid_account"); !ok || number != 1002 {
		t.Fatalf("较晚注册的账号应为用户1002: number=%d ok=%v", number, ok)
	}
}

func TestUserIdentityConcurrentAllocationIsUnique(t *testing.T) {
	svc, err := NewUserIdentityService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	const total = 64
	numbers := make(chan int64, total)
	errs := make(chan error, total)
	var wg sync.WaitGroup
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			number, err := svc.GetOrCreate(fmt.Sprintf("openid_%02d", index), int64(index+1))
			if err != nil {
				errs <- err
				return
			}
			numbers <- number
		}(i)
	}
	wg.Wait()
	close(numbers)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}

	seen := make(map[int64]bool, total)
	for number := range numbers {
		if seen[number] {
			t.Fatalf("并发分配产生重复编号: %d", number)
		}
		seen[number] = true
	}
	if len(seen) != total || !seen[1001] || !seen[1001+total-1] {
		t.Fatalf("并发编号边界错误: count=%d first=%v last=%v", len(seen), seen[1001], seen[1001+total-1])
	}
}

func TestUserIdentityCorruptionNeverSilentlyRebuilds(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "user_identities.json"), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewUserIdentityService(dir); err == nil {
		t.Fatal("编号表损坏时必须报错，禁止静默重建导致 OpenID 编号重排")
	}
}

func TestAnyWechatMessageRecordsFirstOpenID(t *testing.T) {
	dir := t.TempDir()
	identities, err := NewUserIdentityService(dir)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewWechatService(&config.Config{DefaultCodeExpire: 180}, nil, nil, nil, identities)
	body := []byte(`<xml><ToUserName><![CDATA[gh_test]]></ToUserName><FromUserName><![CDATA[openid_help_only]]></FromUserName><CreateTime>1</CreateTime><MsgType><![CDATA[text]]></MsgType><Content><![CDATA[帮助]]></Content></xml>`)
	if _, err := svc.ProcessMessage(t.Context(), body); err != nil {
		t.Fatal(err)
	}
	if number, ok := identities.Lookup("openid_help_only"); !ok || number != 1001 {
		t.Fatalf("未取码、只发送普通消息的 OpenID 也必须首次记录: number=%d ok=%v", number, ok)
	}
}
