package categories

import (
	"bytes"
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
	"strings"
	"syscall"
	"time"
)

//go:embed primary.m.txt
var primarySource string

//go:embed primary.py
var primaryMatcher string

// Primary returns only unambiguous individual messages observed in the actual
// Primary UI. It never selects a row or uses model_category as a substitute.
func Primary(ctx context.Context, limit int) (json.RawMessage, error) {
	if runtime.GOOS != "darwin" {
		return nil, fmt.Errorf("Primary feed requires macOS")
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
	f, err := os.OpenFile(filepath.Join(dir, "categories.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if err != syscall.EWOULDBLOCK {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	// Rows must represent messages, not conversation summaries with mixed states.
	check := func() error {
		s := `tell application "System Events" to tell process "Mail"
set mark to value of attribute "AXMenuItemMarkChar" of menu item "Organize by Conversation" of menu "View" of menu bar item "View" of menu bar 1
if mark is missing value then return "ungrouped"
return "grouped"
end tell`
		c := exec.CommandContext(ctx, "/usr/bin/osascript", "-e", s)
		out, e := c.CombinedOutput()
		if e != nil || strings.TrimSpace(string(out)) != "ungrouped" {
			return fmt.Errorf("Primary feed requires Mail View > Organize by Conversation to be off")
		}
		return nil
	}
	if err = check(); err != nil {
		return nil, err
	}
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(primarySource)))[:16]
	binary := filepath.Join(dir, "primary-"+hash)
	if _, err = os.Stat(binary); os.IsNotExist(err) {
		source := binary + ".m"
		if err = os.WriteFile(source, []byte(primarySource), 0600); err != nil {
			return nil, err
		}
		c := exec.CommandContext(ctx, "/usr/bin/clang", "-fobjc-arc", "-framework", "Cocoa", "-framework", "ApplicationServices", source, "-o", binary+".tmp")
		if out, e := c.CombinedOutput(); e != nil {
			return nil, fmt.Errorf("build Primary AX helper: %w: %s", e, out)
		}
		if err = os.Rename(binary+".tmp", binary); err != nil {
			return nil, err
		}
	}
	c := exec.CommandContext(ctx, binary, strconv.Itoa(limit))
	var native bytes.Buffer
	c.Stdout = &native
	var stderr bytes.Buffer
	c.Stderr = &stderr
	if err = c.Run(); err != nil {
		return nil, fmt.Errorf("Primary UI: %w: %s", err, stderr.String())
	}
	if err = check(); err != nil {
		return nil, err
	}
	c = exec.CommandContext(ctx, "/usr/bin/python3", "-c", primaryMatcher)
	c.Stdin = &native
	var output bytes.Buffer
	stderr.Reset()
	c.Stdout = &output
	c.Stderr = &stderr
	if err = c.Run(); err != nil {
		return nil, fmt.Errorf("Primary matching failed: %w: %s", err, stderr.String())
	}
	if !json.Valid(output.Bytes()) {
		return nil, fmt.Errorf("invalid Primary feed JSON")
	}
	return json.RawMessage(output.Bytes()), nil
}
