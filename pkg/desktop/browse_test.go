package desktop

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestBrowseValidation(t *testing.T) {
	for _, q := range []Request{{Op: "mail-list"}, {Op: "mail-list", Mailbox: "inbox", Category: "Primary", Limit: 200}, {Op: "mail-list", Mailbox: "all"}} {
		if e := Validate(q); e != nil {
			t.Fatal(e)
		}
	}
	for _, q := range []Request{{Op: "mail-list", Mailbox: "all", Category: "Primary"}, {Op: "mail-list", Mailbox: "' OR 1"}, {Op: "mail-list", Category: "Bogus"}, {Op: "mail-list", Limit: 201}} {
		if Validate(q) == nil {
			t.Fatalf("accepted invalid request %+v", q)
		}
	}
}
func TestBrowseCursorBindsView(t *testing.T) {
	q := Request{Mailbox: "inbox", Category: "Primary"}
	options, scope, _, e := browseOptions(q, nil)
	if e != nil {
		t.Fatal(e)
	}
	if options["includeTimeSensitive"] != true {
		t.Fatal("Apple inclusion should default on")
	}
	raw, _ := json.Marshal(browseCursor{Offset: 100, Scope: scope})
	q.Cursor = base64.RawURLEncoding.EncodeToString(raw)
	_, _, offset, e := browseOptions(q, nil)
	if e != nil || offset != 100 {
		t.Fatalf("cursor %d %v", offset, e)
	}
	no := false
	q.IncludeTimeSensitive = &no
	if _, _, _, e = browseOptions(q, nil); e == nil {
		t.Fatal("cursor reused across primary inclusion policy")
	}
	q.IncludeTimeSensitive = nil
	q.UnreadOnly = true
	if _, _, _, e = browseOptions(q, nil); e == nil {
		t.Fatal("cursor reused across unread filter")
	}
}
func TestMailboxSafetyAndAllPolicy(t *testing.T) {
	for _, p := range []string{"/INBOX", "/Projects/2026", "/Sent Messages"} {
		if !safeMailboxPath(p) {
			t.Fatal(p)
		}
	}
	for _, p := range []string{"/", "/../INBOX", "/one//two", "no-slash", "/bad\x00"} {
		if safeMailboxPath(p) {
			t.Fatal(p)
		}
	}
	boxes := []browseMailbox{{ID: "sent", URL: "imap://a/Sent", Kind: "sent"}, {ID: "junk", URL: "imap://a/Junk", Kind: "junk"}, {ID: "trash", URL: "imap://a/Trash", Kind: "trash"}, {ID: "draft", URL: "imap://a/Drafts", Kind: "drafts"}}
	o, _, _, e := browseOptions(Request{Mailbox: "all"}, boxes)
	if e != nil {
		t.Fatal(e)
	}
	excluded := o["excludedMailboxes"].([]string)
	if len(excluded) != 3 {
		t.Fatal(excluded)
	}
	o, _, _, e = browseOptions(Request{Mailbox: "junk"}, boxes)
	if e != nil || o["mailbox"] != "imap://a/Junk" {
		t.Fatalf("explicit Junk unavailable %v %v", o, e)
	}
}

func TestSearchCursorScope(t *testing.T) {
	q := Request{Mailbox: "inbox", Query: "invoice"}
	_, scope, _, err := browseOptions(q, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(browseCursor{Offset: 100, Scope: scope})
	q.Cursor = base64.RawURLEncoding.EncodeToString(raw)
	q.Query = "receipt"
	if _, _, _, err = browseOptions(q, nil); err == nil {
		t.Fatal("search cursor reused for different query")
	}
	if Validate(Request{Op: "mail-list", Query: "x\x00y"}) == nil {
		t.Fatal("NUL query allowed")
	}
}
