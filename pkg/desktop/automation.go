package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

func automationLock(ctx context.Context) (func(), error) {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, "Library/Caches/mail-app-cli")
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	f, e := os.OpenFile(filepath.Join(dir, "desktop-automation.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	deadline := time.NewTimer(8 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if e == nil {
			return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
		}
		if e != syscall.EWOULDBLOCK && e != syscall.EAGAIN {
			f.Close()
			return nil, e
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-deadline.C:
			f.Close()
			return nil, errors.New("Mail is busy downloading or processing another request; try again shortly")
		case <-ticker.C:
		}
	}
}

type accountCache struct {
	Updated int64          `json:"updatedAt"`
	Result  map[string]any `json:"result"`
}

func accountCachePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library/Caches/mail-app-cli/desktop-accounts.json")
}
func cachedAccounts() map[string]any {
	raw, e := os.ReadFile(accountCachePath())
	if e != nil {
		return nil
	}
	var c accountCache
	if json.Unmarshal(raw, &c) != nil || time.Now().Unix()-c.Updated > 86400 {
		return nil
	}
	if rows, ok := c.Result["accounts"].([]any); !ok || len(rows) == 0 {
		return nil
	}
	return c.Result
}
func cacheAccounts(result map[string]any) {
	rows, ok := result["accounts"].([]any)
	if !ok || len(rows) == 0 {
		return
	}
	path := accountCachePath()
	os.MkdirAll(filepath.Dir(path), 0700)
	raw, e := json.Marshal(accountCache{Updated: time.Now().Unix(), Result: result})
	if e != nil {
		return
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".accounts-")
	if e != nil {
		return
	}
	defer os.Remove(f.Name())
	if _, e = f.Write(raw); e != nil {
		f.Close()
		return
	}
	f.Close()
	os.Rename(f.Name(), path)
}
