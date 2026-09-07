package desktop

import (
	"bytes"
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
				return nil, fmt.Errorf("draft %s cannot be read; preserved on Mac: %w", filepath.Base(path), e)
			}
			var d storedDraft
			if json.Unmarshal(raw, &d) != nil || d.ID+".json" != filepath.Base(path) || d.Revision == "" || !json.Valid(d.Data) {
				return nil, fmt.Errorf("draft %s is unreadable; preserved on Mac", filepath.Base(path))
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
	if exists && (json.Unmarshal(raw, &previous) != nil || previous.ID != q.DraftID || previous.Revision == "" || !json.Valid(previous.Data)) {
		return nil, errors.New("draft is unreadable; preserved on Mac")
	}
	if q.Op == "draft-get" {
		if !exists {
			return nil, errors.New("draft no longer exists")
		}
		return map[string]any{"ok": true, "draft": previous.Data, "revision": previous.Revision, "id": previous.ID}, nil
	}
	// A successful write may lose its SSH acknowledgement. Retrying that exact
	// desired document must succeed without overwriting a newer, different edit.
	// Marshal compacts RawMessage on disk, so compare compacted JSON bytes.
	if exists && q.Op == "draft-put" && json.Valid(q.Draft) {
		var desired, saved bytes.Buffer
		json.Compact(&desired, q.Draft)
		json.Compact(&saved, previous.Data)
		if bytes.Equal(desired.Bytes(), saved.Bytes()) {
			if e = syncDraftDirectory(dir); e != nil {
				return nil, e
			}
			return map[string]any{"ok": true, "revision": previous.Revision, "id": previous.ID}, nil
		}
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
		if e = syncDraftDirectory(dir); e != nil {
			return nil, e
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
	if e = syncDraftDirectory(dir); e != nil {
		return nil, e
	}
	return map[string]any{"ok": true, "revision": d.Revision, "id": d.ID}, nil
}

func syncDraftDirectory(dir string) error {
	directory, e := os.Open(dir)
	if e != nil {
		return fmt.Errorf("cannot confirm durable draft storage: %w", e)
	}
	defer directory.Close()
	if e = directory.Sync(); e != nil {
		return fmt.Errorf("cannot confirm durable draft storage: %w", e)
	}
	return nil
}
