// Package categories drives Mail's category UI without an AI or private database writes.
package categories

import (
	"context"
	_ "embed"
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

//go:embed category.applescript
var script string

var Names = []string{"Primary", "Transactions", "Updates", "Promotions"}

type Result struct {
	MessageID  int64    `json:"message_id"`
	Scope      string   `json:"scope"`
	Categories []string `json:"categories,omitempty"`
	SenderRule string   `json:"sender_rule,omitempty"`
	Verified   bool     `json:"verified"`
}

func ValidName(name string, automatic bool) bool {
	if automatic && name == "Automatically" {
		return true
	}
	for _, n := range Names {
		if n == name {
			return true
		}
	}
	return false
}

// Parse accepts only the complete, expected UI report; partial automation is an error.
func Parse(operation, output string, id int64) (Result, error) {
	r := Result{MessageID: id, Scope: "message", Categories: []string{}, Verified: true}
	want := append([]string{}, Names...)
	if operation == "inspect" {
		want = append([]string{"Automatically"}, want...)
		r.Scope = "sender"
	}
	lines := strings.Split(strings.TrimRight(output, "\r\n"), "\n")
	if len(lines) != len(want) {
		return Result{}, fmt.Errorf("incomplete category report")
	}
	for i, line := range lines {
		fields := strings.Split(line, "\t")
		n := 2
		if operation == "inspect" {
			n = 3
		}
		if len(fields) != n || fields[0] != want[i] || (fields[1] != "true" && fields[1] != "false") {
			return Result{}, fmt.Errorf("invalid category report row %d", i+1)
		}
		if operation == "membership" && fields[1] == "true" {
			r.Categories = append(r.Categories, fields[0])
		}
		if operation == "inspect" && fields[2] != "" {
			if fields[2] != "✓" || r.SenderRule != "" {
				return Result{}, fmt.Errorf("ambiguous sender category rule")
			}
			r.SenderRule = fields[0]
		}
	}
	if operation == "inspect" && r.SenderRule == "" {
		return Result{}, fmt.Errorf("no sender category rule found")
	}
	return r, nil
}

func run(ctx context.Context, args ...string) (string, error) {
	c := exec.CommandContext(ctx, "/usr/bin/osascript", append([]string{"-"}, args...)...)
	c.Stdin = strings.NewReader(script)
	out, err := c.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("Mail category automation: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return strings.ReplaceAll(string(out), "\r\n", "\n"), nil
}

// Execute serializes UI operations. Read operations require an already-read inbox
// message, because selecting unread mail can change its read status.
func Execute(ctx context.Context, operation string, id int64, destination string) (Result, error) {
	if operation != "inspect" && operation != "membership" && operation != "apply" {
		return Result{}, fmt.Errorf("unknown operation")
	}
	if id <= 0 {
		return Result{}, fmt.Errorf("message ID must be positive")
	}
	if operation == "apply" && !ValidName(destination, true) {
		return Result{}, fmt.Errorf("unknown category %q", destination)
	}
	if runtime.GOOS != "darwin" {
		return Result{}, fmt.Errorf("category automation requires macOS; run this CLI on your Mac over SSH")
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return Result{}, err
	}
	dir = filepath.Join(dir, "mail-app-cli")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return Result{}, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "categories.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return Result{}, err
	}
	defer f.Close()
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if err != syscall.EWOULDBLOCK {
			return Result{}, err
		}
		select {
		case <-ctx.Done():
			return Result{}, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	args := []string{operation, strconv.FormatInt(id, 10)}
	if operation == "apply" {
		args = append(args, destination)
	}
	out, err := run(ctx, args...)
	if err != nil {
		return Result{}, err
	}
	if operation == "apply" {
		out, err = run(ctx, "inspect", args[1])
		if err != nil {
			return Result{}, fmt.Errorf("action attempted but verification failed: %w", err)
		}
		operation = "inspect"
	}
	r, err := Parse(operation, out, id)
	if err == nil && destination != "" && r.SenderRule != destination {
		return r, fmt.Errorf("category action not verified: wanted %s, got %s; inspect Mail for a dialog", destination, r.SenderRule)
	}
	return r, err
}
