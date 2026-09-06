package categories

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
)

//go:embed native.m.txt
var nativeCategorySource string

// NativeInspect reads persisted categories through Apple's own resolver. It is
// deliberately separate from Primary: it reports all categories and raw metadata.
func NativeInspect(ctx context.Context, limit int) (json.RawMessage, error) {
	return nativeRead(ctx, limit, false)
}

// NativePrimary evaluates Apple's Primary predicate on current persisted state.
func NativePrimary(ctx context.Context, limit int) (json.RawMessage, error) {
	raw, err := nativeRead(ctx, limit, true)
	if err != nil {
		return nil, err
	}
	return formatNativePrimary(raw)
}

func formatNativePrimary(raw []byte) (json.RawMessage, error) {
	var result struct {
		Verified bool                     `json:"verified"`
		Scanned  int                      `json:"scanned"`
		Messages []map[string]interface{} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &result); err != nil || !result.Verified {
		return nil, fmt.Errorf("native Primary result was not verified")
	}
	messages := make([]map[string]interface{}, 0, len(result.Messages))
	seen := make(map[string]bool)
	for _, m := range result.Messages {
		mailbox, mok := m["mailboxUrl"].(string)
		remote, rok := m["remoteId"].(string)
		if !mok || !rok || mailbox == "" || remote == "" || m["primary"] != true {
			return nil, fmt.Errorf("incomplete native Primary identity")
		}
		if _, ok := m["unread"].(bool); !ok {
			return nil, fmt.Errorf("invalid native unread state")
		}
		if _, ok := m["timeSensitive"].(bool); !ok {
			return nil, fmt.Errorf("invalid native time-sensitive state")
		}
		id := fmt.Sprintf("%x", sha256.Sum256([]byte(mailbox+"\x00"+remote)))
		if seen[id] {
			continue
		}
		seen[id] = true
		sender := m["senderName"]
		if sender == nil || sender == "" {
			sender = m["sender"]
		}
		messages = append(messages, map[string]interface{}{
			"id":      id,
			"localId": m["localId"], "mailboxUrl": mailbox, "remoteId": remote,
			"rfcMessageId": m["rfcMessageId"], "category": "Primary", "sender": sender,
			"subject": m["subject"], "received": m["received"], "unread": m["unread"], "timeSensitive": m["timeSensitive"],
		})
	}
	return json.Marshal(map[string]interface{}{"verified": true, "backend": "native", "messages": messages,
		"coverage": fmt.Sprintf("Recent Primary mail · %d messages · native Apple categories", len(messages)),
		"scanned":  result.Scanned, "matched": len(messages), "ambiguous": 0, "unmatched": 0})
}

func nativeRead(ctx context.Context, limit int, primaryOnly bool) (json.RawMessage, error) {
	if runtime.GOOS != "darwin" {
		return nil, fmt.Errorf("native Mail categories require macOS")
	}
	if limit < 1 || limit > 1000 {
		return nil, fmt.Errorf("limit must be 1–1000")
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	dir = filepath.Join(dir, "mail-app-cli")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(nativeCategorySource)))[:16]
	binary := filepath.Join(dir, "native-categories-"+hash)
	if _, err = os.Stat(binary); os.IsNotExist(err) {
		work, e := os.MkdirTemp(dir, "native-build-")
		if e != nil {
			return nil, e
		}
		defer os.RemoveAll(work)
		source := filepath.Join(work, "native.m")
		if e = os.WriteFile(source, []byte(nativeCategorySource), 0600); e != nil {
			return nil, e
		}
		built := filepath.Join(work, "helper")
		c := exec.CommandContext(ctx, "/usr/bin/clang", "-framework", "Foundation", "-lsqlite3", source, "-o", built)
		if out, e := c.CombinedOutput(); e != nil {
			return nil, fmt.Errorf("native helper build: %w: %s", e, out)
		}
		if e = os.Rename(built, binary); e != nil {
			return nil, e
		}
	} else if err != nil {
		return nil, err
	}
	args := []string{strconv.Itoa(limit)}
	if primaryOnly {
		args = append(args, "primary")
	}
	c := exec.CommandContext(ctx, binary, args...)
	out, err := c.Output()
	if err != nil {
		if failure, ok := err.(*exec.ExitError); ok {
			reasons := map[int]string{2: "invalid arguments", 3: "required Apple framework selectors unavailable", 4: "Mail database not readable through a read-only connection", 5: "unsupported Mail database schema", 6: "unsupported category or incomplete database read", 7: "JSON encoding failed", 8: "unsupported Apple framework behavior"}
			if reason, found := reasons[failure.ExitCode()]; found {
				return nil, fmt.Errorf("native category read: %s", reason)
			}
		}
		return nil, fmt.Errorf("native category read failed: %w", err)
	}
	if !json.Valid(out) {
		return nil, fmt.Errorf("invalid native category result")
	}
	return json.RawMessage(out), nil
}
