package desktop

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/intelligrit/mail-app-cli/pkg/categories"
)

// Browsing is a separate protocol: the legacy Primary-only list remains unchanged.
func validateBrowse(q Request) error {
	if len(q.Query) > 1024 || strings.ContainsRune(q.Query, 0) {
		return errors.New("search query is too long or invalid")
	}
	if q.Limit < 0 || q.Limit > 200 {
		return errors.New("limit must be 1–200")
	}
	if len(q.Cursor) > 1024 {
		return errors.New("invalid browsing cursor")
	}
	if q.Mailbox != "" && q.Mailbox != "inbox" && q.Mailbox != "all" && !hexIdentity(q.Mailbox) {
		return errors.New("invalid mailbox")
	}
	switch q.Category {
	case "", "all", "Primary", "Transactions", "Updates", "Promotions":
	default:
		return errors.New("invalid category")
	}
	if q.Category != "" && q.Category != "all" && q.Mailbox != "" && q.Mailbox != "inbox" {
		return errors.New("categories apply to Inbox only")
	}
	return nil
}
func hexIdentity(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, c := range value {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}
func safeMailboxPath(value string) bool {
	if !strings.HasPrefix(value, "/") || value == "/" || strings.ContainsAny(value, "\x00\r\n") {
		return false
	}
	for _, part := range strings.Split(value[1:], "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

type browseMailbox struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	AccountID string `json:"accountId"`
	Kind      string `json:"kind"`
	URL       string `json:"-"`
}

func mailboxKind(name string) string {
	switch strings.ToLower(name) {
	case "inbox":
		return "inbox"
	case "trash", "deleted messages", "deleted items", "papierkorb":
		return "trash"
	case "junk", "junk email", "junk e-mail", "spam", "unerwünscht":
		return "junk"
	case "drafts", "entwürfe":
		return "drafts"
	case "sent", "sent messages", "sent items", "gesendet":
		return "sent"
	case "archive", "archiv":
		return "archive"
	}
	return "folder"
}
func readMailboxes(ctx context.Context) ([]browseMailbox, error) {
	home, e := os.UserHomeDir()
	if e != nil {
		return nil, e
	}
	cmd := exec.CommandContext(ctx, "/usr/bin/sqlite3", "-readonly", "-json", filepath.Join(home, "Library/Mail/V10/MailData/Envelope Index"))
	cmd.Stdin = strings.NewReader("SELECT DISTINCT b.url AS url FROM mailboxes b JOIN messages m ON m.mailbox=b.rowid WHERE m.deleted=0 AND m.remote_id>0 ORDER BY b.url;")
	raw, e := cmd.Output()
	if e != nil {
		return nil, errors.New("Mail index is unavailable")
	}
	var rows []struct {
		URL string `json:"url"`
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return []browseMailbox{}, nil
	}
	if json.Unmarshal(raw, &rows) != nil {
		return nil, errors.New("invalid mailbox index response")
	}
	result := []browseMailbox{}
	for _, r := range rows {
		u, e := url.Parse(r.URL)
		if e != nil || u.Host == "" || !safeMailboxPath(u.Path) {
			continue
		}
		name := strings.TrimPrefix(u.Path, "/")
		result = append(result, browseMailbox{ID: fmt.Sprintf("%x", sha256.Sum256([]byte(r.URL))), Name: name, AccountID: u.Host, Kind: mailboxKind(name), URL: r.URL})
	}
	return result, nil
}

type browseCursor struct {
	Offset int    `json:"offset"`
	Scope  string `json:"scope"`
}

func browseOptions(q Request, boxes []browseMailbox) (map[string]any, string, int, error) {
	mailbox := q.Mailbox
	if mailbox == "" {
		mailbox = "inbox"
	}
	category := q.Category
	if category == "" {
		category = "all"
	}
	include := q.IncludeTimeSensitive == nil || *q.IncludeTimeSensitive
	excluded := []string{}
	found := mailbox == "inbox" || mailbox == "all"
	for _, b := range boxes {
		if b.Kind == "junk" || b.Kind == "trash" || b.Kind == "drafts" {
			excluded = append(excluded, b.URL)
		}
		if b.ID == mailbox {
			mailbox = b.URL
			found = true
		}
	}
	if !found {
		return nil, "", 0, errors.New("mailbox no longer exists; refresh mailboxes")
	}
	options := map[string]any{"mailbox": mailbox, "category": category, "includeTimeSensitive": include, "unreadOnly": q.UnreadOnly, "excludedMailboxes": excluded, "query": strings.TrimSpace(q.Query)}
	if q.Op == "thread-list" {
		allowed := []string{}
		for _, b := range boxes {
			if b.AccountID == q.Account && b.Kind != "junk" && b.Kind != "trash" && b.Kind != "drafts" {
				allowed = append(allowed, b.URL)
			}
		}
		options = map[string]any{"mailbox": "all", "category": "all", "threadLocalId": q.LocalID, "threadMailboxes": allowed}
	}
	scopeData, _ := json.Marshal(options)
	scope := fmt.Sprintf("%x", sha256.Sum256(scopeData))
	offset := 0
	if q.Cursor != "" {
		data, e := base64.RawURLEncoding.DecodeString(q.Cursor)
		var cursor browseCursor
		if e != nil || json.Unmarshal(data, &cursor) != nil || cursor.Offset < 0 || cursor.Offset > 10000000 || cursor.Scope != scope {
			return nil, "", 0, errors.New("cursor does not match this view; refresh")
		}
		offset = cursor.Offset
	}
	options["offset"] = offset
	return options, scope, offset, nil
}
func browseOperation(ctx context.Context, q Request) (map[string]any, error) {
	boxes, e := readMailboxes(ctx)
	if e != nil {
		return nil, e
	}
	policy := "All Mail includes indexed server mailboxes, excluding recognized Junk, Trash and Drafts folders; unknown/custom folder roles are included. Empty and local-only mailboxes are not listed."
	if q.Op == "mailboxes" {
		return map[string]any{"ok": true, "mailboxes": boxes, "allMailPolicy": policy}, nil
	}
	if q.Op == "thread-list" {
		if e := resolve(ctx, &q); e != nil {
			return nil, e
		}
	}
	options, scope, offset, e := browseOptions(q, boxes)
	if e != nil {
		return nil, e
	}
	limit := q.Limit
	if limit == 0 {
		limit = 100
	}
	raw, e := categories.NativeBrowse(ctx, limit+1, options)
	if e != nil {
		return nil, e
	}
	var result struct {
		Verified bool             `json:"verified"`
		Messages []map[string]any `json:"messages"`
	}
	if json.Unmarshal(raw, &result) != nil || !result.Verified {
		return nil, errors.New("unverified category response")
	}
	next := ""
	if len(result.Messages) > limit {
		result.Messages = result.Messages[:limit]
		encoded, _ := json.Marshal(browseCursor{Offset: offset + limit, Scope: scope})
		next = base64.RawURLEncoding.EncodeToString(encoded)
	}
	messages := []map[string]any{}
	for _, m := range result.Messages {
		mailbox, mok := m["mailboxUrl"].(string)
		remote, rok := m["remoteId"].(string)
		if !mok || !rok || mailbox == "" || remote == "" {
			return nil, errors.New("incomplete message identity")
		}
		m["id"] = fmt.Sprintf("%x", sha256.Sum256([]byte(mailbox+"\x00"+remote)))
		u, err := url.Parse(mailbox)
		if err != nil || u.Host == "" {
			return nil, errors.New("invalid account identity")
		}
		m["accountId"] = u.Host
		m["threadId"] = categories.ThreadIdentity(mailbox, m["conversationId"], m["id"].(string))
		delete(m, "conversationId")
		m["mailboxId"] = fmt.Sprintf("%x", sha256.Sum256([]byte(mailbox)))
		m["underlyingCategory"] = m["category"]
		m["senderAddress"] = m["sender"]
		if name, ok := m["senderName"].(string); ok && name != "" {
			m["sender"] = name
		}
		m["primaryReason"] = ""
		if m["primary"] == true {
			if m["category"] == "Primary" {
				m["primaryReason"] = "category"
			} else {
				m["primaryReason"] = "time-sensitive"
			}
		}
		messages = append(messages, m)
	}
	return map[string]any{"ok": true, "verified": true, "backend": "native", "messages": messages, "nextCursor": next, "allMailPolicy": policy, "coverage": fmt.Sprintf("%d messages · native Apple categories", len(messages)), "pagination": "offset; refresh after mailbox changes"}, nil
}
