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
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Read complete cached messages without sending an AppleEvent. Mail's source()
// downloads missing parts synchronously and blocks unrelated Mail automation.
func messagePaths(ctx context.Context, tree fs.FS, name string) ([]string, error) {
	paths := []string{}
	e := fs.WalkDir(tree, ".", func(dir string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() {
			return nil
		}
		if d.Name() == "Attachments" {
			return fs.SkipDir
		}
		if d.Name() == "Messages" {
			// Do not enumerate the 100,000+ cached message files. There are only
			// a small number of bucket directories; check exact filenames.
			for _, candidate := range []string{name, strings.TrimSuffix(name, ".emlx") + ".partial.emlx"} {
				file := path.Join(dir, candidate)
				if st, e := fs.Stat(tree, file); e == nil && st.Mode().IsRegular() {
					paths = append(paths, file)
				}
			}
			if len(paths) > 1 {
				return fs.SkipAll
			}
			return fs.SkipDir
		}
		return ctx.Err()
	})
	return paths, e
}
func localMessage(ctx context.Context, q Request) map[string]any {
	home, _ := os.UserHomeDir()
	root := filepath.Join(home, "Library/Mail/V10", q.Account)
	// Prefer the mailbox's conventional disk location. Fall back to the account
	// tree for nested/encoded names; index row IDs are unique across all mailboxes.
	mailboxPath := q.MailboxPath
	if mailboxPath == "" {
		mailboxPath = "INBOX"
	}
	candidate := root
	for _, part := range strings.Split(mailboxPath, "/") {
		candidate = filepath.Join(candidate, part+".mbox")
	}
	if st, err := os.Stat(candidate); err == nil && st.IsDir() {
		root = candidate
	}

	if result := cachedLocalMessage(q, root); result != nil {
		return result
	}
	name := strconv.FormatInt(q.LocalID, 10) + ".emlx"
	paths, e := messagePaths(ctx, os.DirFS(root), name)
	if e != nil || len(paths) != 1 {
		return nil
	}
	paths[0] = filepath.Join(root, paths[0])
	stamp := sourceStamp(paths[0], q.LocalID)
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
	content := cachedContent{Attachments: []Attachment{}, Missing: []string{}}
	attachmentRoot := filepath.Join(filepath.Dir(filepath.Dir(paths[0])), "Attachments", strconv.FormatInt(q.LocalID, 10))
	cachedMIME(m.Header, m.Body, attachmentRoot, "", 0, &content)
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
		"to": addresses("To"), "cc": addresses("Cc"), "read": q.StoredRead, "body": content.Plain, "html": content.HTML, "attachments": content.Attachments, "missing": content.Missing, "complete": len(content.Missing) == 0,
	}}
	if len(content.Missing) == 0 && stamp != "" && stamp == sourceStamp(paths[0], q.LocalID) {
		writeReadCache(q.ID, "body", sourceEntry{Path: paths[0], Stamp: stamp, Checked: time.Now(), Result: result})
	}
	return result
}
