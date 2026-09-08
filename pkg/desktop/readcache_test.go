package desktop

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHintIsVerifiedAndStaleLocatorFallsBack(t *testing.T) {
	if _, err := os.Stat("/usr/bin/sqlite3"); err != nil {
		t.Skip("sqlite3 required")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	db := filepath.Join(home, "Library/Mail/V10/MailData/Envelope Index")
	os.MkdirAll(filepath.Dir(db), 0700)
	sql := `CREATE TABLE messages(mailbox INTEGER,remote_id INTEGER,read INTEGER,date_received INTEGER,deleted INTEGER); CREATE TABLE mailboxes(url TEXT); INSERT INTO mailboxes VALUES('imap://account/INBOX'); INSERT INTO messages VALUES(1,100,0,1,0),(1,200,1,2,0);`
	if out, e := exec.Command("/usr/bin/sqlite3", db, sql).CombinedOutput(); e != nil {
		t.Fatalf("fixture: %s %v", out, e)
	}
	id := fmt.Sprintf("%x", sha256.Sum256([]byte("imap://account/INBOX\x00100")))
	q := Request{ID: id, HintLocalID: 2}
	if e := resolve(context.Background(), &q); e != nil || q.LocalID != 1 {
		t.Fatalf("stale hint didn't recover: %v", e)
	}
	q = Request{ID: id}
	if e := resolve(context.Background(), &q); e != nil || q.LocalID != 1 {
		t.Fatalf("locator failed: %v", e)
	}
	q = Request{ID: strings.Repeat("b", 64), HintLocalID: 1}
	if resolve(context.Background(), &q) == nil {
		t.Fatal("trusted unverified client identity")
	}
	exec.Command("/usr/bin/sqlite3", db, `INSERT INTO messages VALUES(1,100,0,1,0);`).Run()
	q = Request{ID: id, HintLocalID: 1}
	if e := resolve(context.Background(), &q); e == nil || !strings.Contains(e.Error(), "ambiguous") {
		t.Fatalf("lost ambiguity protection: %v", e)
	}
}

func TestParsedCacheTracksSourceAndAttachments(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	root := t.TempDir()
	path := filepath.Join(root, "Messages", "7.emlx")
	os.MkdirAll(filepath.Dir(path), 0700)
	os.WriteFile(path, []byte("fixture"), 0600)
	id := strings.Repeat("a", 64)
	q := Request{ID: id, LocalID: 7, StoredRead: true, Received: 1}
	result := map[string]any{"message": map[string]any{"id": id, "complete": true, "read": false}}
	writeReadCache(id, "body", sourceEntry{Path: path, Stamp: sourceStamp(path, 7), Checked: time.Now(), Result: result})
	got := cachedLocalMessage(q, root)
	if got == nil || got["message"].(map[string]any)["read"] != true {
		t.Fatal("cache miss or stale read flag")
	}
	if cachedLocalMessage(q, t.TempDir()) != nil {
		t.Fatal("accepted another mailbox root")
	}
	attachment := filepath.Join(root, "Attachments", "7", "part")
	os.MkdirAll(filepath.Dir(attachment), 0700)
	os.WriteFile(attachment, []byte("downloaded"), 0600)
	if cachedLocalMessage(q, root) != nil {
		t.Fatal("missed newly downloaded MIME part")
	}
	writeReadCache(id, "body", sourceEntry{Path: path, Stamp: sourceStamp(path, 7), Checked: time.Now(), Result: result})
	os.WriteFile(path, []byte("changed fixture"), 0600)
	if cachedLocalMessage(q, root) != nil {
		t.Fatal("missed source change")
	}
}
func TestLocatorAndExpiry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	id := fmt.Sprintf("%064x", 7)
	writeReadCache(id, "locator", int64(7))
	if cachedLocator(id) != 7 {
		t.Fatal("locator not persistent")
	}
	if readCachePath("../../escape", "body") != "" {
		t.Fatal("accepted path traversal")
	}
	info, e := os.Stat(readCachePath(id, "locator"))
	if e != nil || info.Mode().Perm() != 0600 {
		t.Fatal("cache not private")
	}
	t.Setenv("PRIMARY_MAIL_CACHE_DISABLE", "1")
	if cachedLocator(id) != 0 {
		t.Fatal("rollback switch ignored")
	}
}
func TestDeferredAttachmentsRetainInline(t *testing.T) {
	result := map[string]any{"message": map[string]any{"html": "<img src=\"cid:logo\">", "attachments": []Attachment{
		{Name: "logo", CID: "logo", Data: "aGVsbG8="}, {Name: "pdf", Data: "cGRm"},
	}}}
	deferReadAttachments(result)
	items := result["message"].(map[string]any)["attachments"].([]map[string]any)
	if items[0]["data"] != "aGVsbG8=" || items[1]["data"] != nil || items[1]["deferred"] != true {
		t.Fatal("incorrect deferred payload")
	}
}
