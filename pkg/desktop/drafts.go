package desktop

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type storedDraft struct {
	ID       string          `json:"id"`
	Revision string          `json:"revision"`
	Updated  int64           `json:"updatedAt"`
	Data     json.RawMessage `json:"draft"`
}

func draftOperation(q Request) (map[string]any, error) {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, "Library/Application Support/Primary Mail/Drafts")
	return draftInDir(dir, q)
}
func draftInDir(dir string, q Request) (map[string]any, error) {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	lock, e := os.OpenFile(filepath.Join(dir, ".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	defer lock.Close()
	if e = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); e != nil {
		return nil, e
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	if q.Op == "draft-list" {
		paths, e := filepath.Glob(filepath.Join(dir, "*.json"))
		if e != nil {
			return nil, e
		}
		result := []map[string]any{}
		for _, path := range paths {
			raw, e := os.ReadFile(path)
			if e != nil {
				continue
			}
			var d storedDraft
			if json.Unmarshal(raw, &d) != nil {
				continue
			}
			var data struct {
				Subject string `json:"subject"`
			}
			json.Unmarshal(d.Data, &data)
			result = append(result, map[string]any{"id": d.ID, "updatedAt": d.Updated, "subject": data.Subject})
		}
		return map[string]any{"ok": true, "drafts": result}, nil
	}
	if len(q.DraftID) != 32 || strings.Trim(q.DraftID, "0123456789abcdef") != "" {
		return nil, errors.New("invalid draft ID")
	}
	path := filepath.Join(dir, q.DraftID+".json")
	raw, e := os.ReadFile(path)
	var previous storedDraft
	if e != nil && !os.IsNotExist(e) {
		return nil, e
	}
	exists := e == nil
	if exists && json.Unmarshal(raw, &previous) != nil {
		return nil, errors.New("draft is unreadable; preserved on Mac")
	}
	if q.Op == "draft-get" {
		if !exists {
			return nil, errors.New("draft no longer exists")
		}
		return map[string]any{"ok": true, "draft": previous.Data, "revision": previous.Revision, "id": previous.ID}, nil
	}
	if exists && q.Revision != previous.Revision {
		return nil, errors.New("draft changed on another device; reopen it before editing further")
	}
	if !exists && q.Revision != "" {
		return nil, errors.New("draft was removed on another device")
	}
	if q.Op == "draft-delete" {
		if exists {
			if e = os.Remove(path); e != nil {
				return nil, e
			}
		}
		return map[string]any{"ok": true}, nil
	}
	if q.Op != "draft-put" || !json.Valid(q.Draft) {
		return nil, errors.New("invalid draft operation")
	}
	d := storedDraft{ID: q.DraftID, Revision: fmt.Sprintf("%x", sha256.Sum256(q.Draft)), Updated: time.Now().Unix(), Data: q.Draft}
	raw, e = json.Marshal(d)
	if e != nil {
		return nil, e
	}
	temp, e := os.CreateTemp(dir, ".draft-")
	if e != nil {
		return nil, e
	}
	defer os.Remove(temp.Name())
	if _, e = temp.Write(raw); e != nil {
		temp.Close()
		return nil, e
	}
	if e = temp.Sync(); e != nil {
		temp.Close()
		return nil, e
	}
	if e = temp.Close(); e != nil {
		return nil, e
	}
	if e = os.Rename(temp.Name(), path); e != nil {
		return nil, e
	}
	if directory, e := os.Open(dir); e == nil {
		directory.Sync()
		directory.Close()
	}
	return map[string]any{"ok": true, "revision": d.Revision, "id": d.ID}, nil
}
