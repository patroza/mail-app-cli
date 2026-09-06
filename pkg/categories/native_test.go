package categories

import (
	"encoding/json"
	"testing"
)

func TestNativePrimaryIdentityAndUnread(t *testing.T) {
	raw := []byte(`{"verified":true,"scanned":3,"messages":[{"localId":"10","mailboxUrl":"imap://account/INBOX","remoteId":"42","primary":true,"unread":true,"timeSensitive":false,"senderName":"Sender"},{"localId":"20","mailboxUrl":"imap://account/INBOX","remoteId":"42","primary":true,"unread":true,"timeSensitive":false}]}`)
	a, err := formatNativePrimary(raw)
	if err != nil {
		t.Fatal(err)
	}
	var d struct {
		Messages []map[string]interface{} `json:"messages"`
	}
	if err = json.Unmarshal(a, &d); err != nil {
		t.Fatal(err)
	}
	if len(d.Messages) != 1 || d.Messages[0]["unread"] != true {
		t.Fatalf("duplicate identity or lost unread: %s", a)
	}
	id := d.Messages[0]["id"]
	b, err := formatNativePrimary([]byte(`{"verified":true,"messages":[{"localId":"999","mailboxUrl":"imap://account/INBOX","remoteId":"42","primary":true,"unread":false,"timeSensitive":true}]}`))
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal(b, &d)
	if d.Messages[0]["id"] != id || d.Messages[0]["unread"] != false {
		t.Fatal("identity changed with local ID/read state")
	}
}

func TestNativePrimaryFailsClosed(t *testing.T) {
	for _, raw := range []string{
		`{"verified":false,"messages":[]}`,
		`{"verified":true,"messages":[{"primary":true}]}`,
		`{"verified":true,"messages":[{"mailboxUrl":"imap://a/INBOX","remoteId":"1","primary":false}]}`,
		`{"verified":true,"messages":[{"mailboxUrl":"imap://a/INBOX","remoteId":"1","primary":true,"unread":1,"timeSensitive":false}]}`,
	} {
		if _, err := formatNativePrimary([]byte(raw)); err == nil {
			t.Fatalf("accepted invalid feed: %s", raw)
		}
	}
}
