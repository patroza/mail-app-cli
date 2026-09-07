// Package desktop exposes a bounded, stdin-only protocol for a Linux Mail UI.
package desktop

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/intelligrit/mail-app-cli/pkg/categories"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

//go:embed desktop.js
var script string

const MaxRequest = 32 << 20

type Request struct {
	Query                string `json:"query,omitempty"`
	Mailbox              string `json:"mailbox,omitempty"`
	Category             string `json:"category,omitempty"`
	IncludeTimeSensitive *bool  `json:"includeTimeSensitive,omitempty"`
	UnreadOnly           bool   `json:"unreadOnly,omitempty"`
	Limit                int    `json:"limit,omitempty"`
	Cursor               string `json:"cursor,omitempty"`
	MailboxPath          string `json:"resolvedMailbox,omitempty"`

	RemoteID    string          `json:"-"`
	StoredRead  bool            `json:"-"`
	Received    int64           `json:"-"`
	DraftID     string          `json:"draftId,omitempty"`
	Revision    string          `json:"revision,omitempty"`
	Draft       json.RawMessage `json:"draft,omitempty"`
	Attachments []Attachment    `json:"attachments,omitempty"`
	Op          string          `json:"op"`
	ID          string          `json:"id"`
	Mode        string          `json:"mode"`
	AccountID   string          `json:"accountId"`
	Sender      string          `json:"sender"`
	To          []string        `json:"to"`
	Cc          []string        `json:"cc"`
	Bcc         []string        `json:"bcc"`
	Subject     string          `json:"subject"`
	Body        string          `json:"body"`
	Read        bool            `json:"read"`
	Account     string          `json:"resolvedAccount,omitempty"`
	LocalID     int64           `json:"localId,omitempty"`
}

func Decode(r io.Reader) (Request, error) {
	var q Request
	raw, err := io.ReadAll(io.LimitReader(r, MaxRequest+1))
	if err != nil {
		return q, err
	}
	if len(raw) > MaxRequest {
		return q, errors.New("message exceeds 32 MiB request limit")
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&q); err != nil {
		return q, errors.New("invalid request")
	}
	if err = dec.Decode(new(any)); err != io.EOF {
		return q, errors.New("invalid trailing request")
	}
	q.Account = ""
	q.LocalID = 0
	q.MailboxPath = ""
	for i := range q.Attachments {
		q.Attachments[i].Path = ""
	}
	return q, nil
}
func Validate(q Request) error {
	switch q.Op {
	case "mailboxes":
		return nil
	case "mail-list":
		return validateBrowse(q)
	case "accounts", "contacts", "list", "draft-list", "draft-get", "draft-put", "draft-delete":
		return nil
	case "thread-list":
		if err := validateBrowse(q); err != nil {
			return err
		}
		if !hexIdentity(q.ID) {
			return errors.New("invalid thread anchor")
		}
		return nil
	case "read", "mark", "cached-images":
	case "send", "preview":
		if err := validateInline(q); err != nil {
			return err
		}
		if q.Mode != "new" && q.Mode != "reply" && q.Mode != "forward" {
			return errors.New("invalid compose mode")
		}
		if len(q.To)+len(q.Cc)+len(q.Bcc) == 0 {
			return errors.New("add at least one recipient")
		}
		if len(q.To)+len(q.Cc)+len(q.Bcc) > 100 {
			return errors.New("too many recipients")
		}
		for _, s := range append(append(append([]string{q.Sender}, q.To...), q.Cc...), q.Bcc...) {
			a, e := mail.ParseAddress(s)
			if e != nil || a.Address != s || !strings.Contains(s, "@") || strings.ContainsAny(s, "\r\n") {
				return errors.New("invalid email address")
			}
		}
		if strings.ContainsAny(q.Subject, "\r\n") {
			return errors.New("subject must be one line")
		}
		if q.Mode == "new" {
			return nil
		}
	default:
		return errors.New("unknown operation")
	}
	if len(q.ID) != 64 {
		return errors.New("invalid message identity")
	}
	for _, c := range q.ID {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return errors.New("invalid message identity")
		}
	}
	return nil
}
func resolve(ctx context.Context, q *Request) error {
	home, _ := os.UserHomeDir()
	sql := `SELECT m.rowid AS localId,b.url AS mailboxUrl,CAST(m.remote_id AS TEXT) AS remoteId,m.read AS wasRead,m.date_received AS received FROM messages m JOIN mailboxes b ON b.rowid=m.mailbox WHERE m.deleted=0 AND m.remote_id>0 ;`
	cmd := exec.CommandContext(ctx, "/usr/bin/sqlite3", "-readonly", "-json", filepath.Join(home, "Library/Mail/V10/MailData/Envelope Index"))
	cmd.Stdin = strings.NewReader(sql)
	raw, err := cmd.Output()
	if err != nil {
		return errors.New("Mail index is unavailable")
	}
	var rows []struct {
		LocalID    int64  `json:"localId"`
		MailboxURL string `json:"mailboxUrl"`
		RemoteID   string `json:"remoteId"`
		WasRead    int    `json:"wasRead"`
		Received   int64  `json:"received"`
	}
	if json.Unmarshal(raw, &rows) != nil {
		return errors.New("invalid Mail index response")
	}
	for _, r := range rows {
		id := fmt.Sprintf("%x", sha256.Sum256([]byte(r.MailboxURL+"\x00"+r.RemoteID)))
		if id == q.ID {
			u, e := url.Parse(r.MailboxURL)
			if e != nil || u.Host == "" {
				return errors.New("invalid mailbox identity")
			}
			if q.LocalID != 0 {
				return errors.New("message identity is ambiguous")
			}
			if !safeMailboxPath(u.Path) {
				return errors.New("unsupported mailbox path")
			}
			q.MailboxPath = strings.TrimPrefix(u.Path, "/")
			q.Account = u.Host
			q.LocalID = r.LocalID
			q.RemoteID = r.RemoteID
			q.StoredRead = r.WasRead != 0
			q.Received = r.Received
		}
	}
	if q.LocalID == 0 {
		return errors.New("message is no longer in this mailbox; refresh the message list")
	}
	return nil
}
func Execute(ctx context.Context, q Request) (map[string]any, error) {
	if e := Validate(q); e != nil {
		return nil, e
	}
	if q.Op == "mailboxes" || q.Op == "mail-list" || q.Op == "thread-list" {
		return browseOperation(ctx, q)
	}
	if strings.HasPrefix(q.Op, "draft-") {
		return draftOperation(q)
	}
	if q.Op == "accounts" {
		if cached := cachedAccounts(); cached != nil {
			return cached, nil
		}
	}
	if q.Op == "list" {
		raw, e := categories.NativePrimary(ctx, 100)
		if e != nil {
			return nil, errors.New("Primary sync unavailable")
		}
		var result map[string]any
		if e = json.Unmarshal(raw, &result); e != nil {
			return nil, e
		}
		result["ok"] = true
		return result, nil
	}
	if q.Op == "contacts" {
		return contacts(ctx)
	}
	if q.Op == "read" || q.Op == "cached-images" || q.Op == "mark" || ((q.Op == "send" || q.Op == "preview") && q.Mode != "new") {
		if e := resolve(ctx, &q); e != nil {
			return nil, e
		}
	}
	if q.Op == "send" || q.Op == "preview" {
		return compose(ctx, q)
	}
	if q.Op == "cached-images" {
		return cachedImages(ctx, q)
	}
	if q.Op == "read" {
		if local := localMessage(ctx, q); local != nil {
			return local, nil
		}
		return nil, errors.New("Message is not cached on the Mac yet; let Mail finish downloading, then retry")
	}
	unlock, e := automationLock(ctx)
	if e != nil {
		return nil, e
	}
	defer unlock()
	data, _ := json.Marshal(q)
	cmd := exec.CommandContext(ctx, "/usr/bin/osascript", "-l", "JavaScript", "-")
	cmd.Stdin = strings.NewReader("const request = " + string(data) + ";\n" + script)
	raw, e := cmd.Output()
	if e != nil {
		return nil, errors.New("Mail automation failed; check the Mac session and permissions")
	}
	var result map[string]any
	if json.Unmarshal(raw, &result) != nil {
		return nil, errors.New("invalid Mail automation response")
	}
	if q.Op == "accounts" && result["ok"] == true {
		cacheAccounts(result)
	}
	if q.Op == "read" && result["ok"] == true {
		return decodeRead(result)
	}
	return result, nil
}
func decodeRead(result map[string]any) (map[string]any, error) {
	if result["ok"] == true {
		msg, ok := result["message"].(map[string]any)
		if !ok {
			return nil, errors.New("missing message")
		}
		source, _ := msg["source"].(string)
		delete(msg, "source")
		if len(source) > 32<<20 {
			return nil, errors.New("message exceeds 32 MiB reading limit")
		}
		parsed, e := mail.ReadMessage(strings.NewReader(source))
		if e == nil {
			plain, rich := parts(parsed.Header, parsed.Body, 0)
			if plain != "" {
				msg["body"] = plain
			}
			msg["html"] = rich
		}
		attachmentMessage, e := mail.ReadMessage(strings.NewReader(source))
		attachments := []Attachment{}
		if e != nil {
			return nil, errors.New("cannot decode message attachments")
		}
		if e = extractAttachments(attachmentMessage.Header, attachmentMessage.Body, 0, &attachments); e != nil {
			return nil, e
		}
		msg["attachments"] = attachments
	}
	return result, nil
}
func parts(header mail.Header, r io.Reader, depth int) (string, string) {
	if depth > 15 {
		return "", ""
	}
	kind, params, e := mime.ParseMediaType(header.Get("Content-Type"))
	if e != nil {
		kind = "text/plain"
	}
	disposition, _, _ := mime.ParseMediaType(header.Get("Content-Disposition"))
	if disposition == "attachment" {
		return "", ""
	}
	if strings.HasPrefix(kind, "multipart/") {
		var plain, rich string
		mr := multipart.NewReader(r, params["boundary"])
		for i := 0; i < 200; i++ {
			p, e := mr.NextPart()
			if e != nil {
				break
			}
			a, b := parts(mail.Header(p.Header), p, depth+1)
			if plain == "" {
				plain = a
			}
			if rich == "" {
				rich = b
			}
		}
		return plain, rich
	}
	if kind != "text/plain" && kind != "text/html" {
		return "", ""
	}
	switch strings.ToLower(header.Get("Content-Transfer-Encoding")) {
	case "base64":
		r = base64.NewDecoder(base64.StdEncoding, r)
	case "quoted-printable":
		r = quotedprintable.NewReader(r)
	}
	body, e := io.ReadAll(io.LimitReader(r, 4<<20))
	if e != nil {
		return "", ""
	}
	charset := strings.ToLower(params["charset"])
	if charset == "iso-8859-1" {
		var b strings.Builder
		for _, c := range body {
			b.WriteRune(rune(c))
		}
		body = []byte(b.String())
	}
	if charset != "" && charset != "utf-8" && charset != "us-ascii" && charset != "iso-8859-1" {
		if len(charset) > 50 || strings.HasPrefix(charset, "-") || strings.ContainsAny(charset, "/\\\x00\r\n ") {
			return "", ""
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "/usr/bin/iconv", "-f", charset, "-t", "UTF-8")
		cmd.Stdin = strings.NewReader(string(body))
		converted, e := cmd.Output()
		if e != nil {
			return "", ""
		}
		body = converted
	}
	if kind == "text/html" {
		return "", string(body)
	}
	return string(body), ""
}
func contacts(ctx context.Context) (map[string]any, error) {
	home, _ := os.UserHomeDir()
	raw, e := exec.CommandContext(ctx, filepath.Join(home, ".blip/bin/contacts"), "--json", "dump").Output()
	if e != nil {
		return nil, errors.New("Contacts unavailable")
	}
	var rows []struct {
		Name   string `json:"name"`
		Emails []struct {
			Address string `json:"address"`
		} `json:"emails"`
	}
	if json.Unmarshal(raw, &rows) != nil {
		return nil, errors.New("invalid contacts response")
	}
	result := []map[string]string{}
	seen := map[string]bool{}
	for _, r := range rows {
		for _, a := range r.Emails {
			if _, e := mail.ParseAddress(a.Address); e == nil && !seen[a.Address] {
				seen[a.Address] = true
				result = append(result, map[string]string{"name": r.Name, "email": a.Address})
			}
		}
	}
	return map[string]any{"ok": true, "contacts": result}, nil
}
