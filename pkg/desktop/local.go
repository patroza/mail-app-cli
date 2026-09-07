package desktop

import (
	"bufio"
	"context"
	"io"
	"io/fs"
	"mime"
	"net/mail"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Read complete cached messages without sending an AppleEvent. Mail's source()
// downloads missing parts synchronously and blocks unrelated Mail automation.
func localMessage(ctx context.Context, q Request) map[string]any {
	home, _ := os.UserHomeDir()
	root := filepath.Join(home, "Library/Mail/V10", q.Account, "INBOX.mbox")
	name := strconv.FormatInt(q.LocalID, 10) + ".emlx"
	paths := []string{}
	walked := 0
	e := filepath.WalkDir(root, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		walked++
		if walked > 100000 {
			return fs.SkipAll
		}
		if !d.IsDir() && d.Name() == name && filepath.Base(filepath.Dir(path)) == "Messages" {
			paths = append(paths, path)
		}
		return ctx.Err()
	})
	if e != nil || len(paths) != 1 {
		return nil
	}
	f, e := os.Open(paths[0])
	if e != nil {
		return nil
	}
	defer f.Close()
	reader := bufio.NewReader(f)
	line, e := reader.ReadString('\n')
	if e != nil || len(line) > 20 {
		return nil
	}
	size, e := strconv.Atoi(strings.TrimSpace(line))
	if e != nil || size < 1 || size > 32<<20 {
		return nil
	}
	source := make([]byte, size)
	if _, e = io.ReadFull(reader, source); e != nil {
		return nil
	}
	trailer, e := io.ReadAll(io.LimitReader(reader, 1<<20))
	if e != nil {
		return nil
	}
	cmd := exec.CommandContext(ctx, "/usr/bin/plutil", "-extract", "remote-id", "raw", "-o", "-", "-")
	cmd.Stdin = strings.NewReader(string(trailer))
	remote, e := cmd.Output()
	if e != nil || strings.TrimSpace(string(remote)) != q.RemoteID {
		return nil
	}
	m, e := mail.ReadMessage(strings.NewReader(string(source)))
	if e != nil {
		return nil
	}
	// Partial-content markers can also occur in .emlx files. Never silently lose
	// externally stored MIME parts; let Mail reconstruct those through source().
	if strings.Contains(strings.ToLower(string(source)), "x-apple-content-length:") {
		return nil
	}
	decode := func(s string) string {
		if v, e := new(mime.WordDecoder).DecodeHeader(s); e == nil {
			return v
		}
		return s
	}
	addresses := func(field string) []string {
		result := []string{}
		rows, _ := m.Header.AddressList(field)
		for _, r := range rows {
			result = append(result, r.Address)
		}
		return result
	}
	result := map[string]any{"ok": true, "backend": "local-emlx", "message": map[string]any{
		"id": q.ID, "accountId": q.Account, "subject": decode(m.Header.Get("Subject")), "sender": decode(m.Header.Get("From")),
		"replyTo": decode(m.Header.Get("Reply-To")), "date": time.Unix(q.Received, 0).UTC().Format(time.RFC3339),
		"to": addresses("To"), "cc": addresses("Cc"), "read": q.StoredRead, "body": "", "source": string(source),
	}}
	return result
}
