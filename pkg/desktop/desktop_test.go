package desktop

import (
	"encoding/base64"
	"encoding/json"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDraftConflictsPreserveSavedVersion(t *testing.T) {
	dir := t.TempDir()
	q := Request{Op: "draft-put", DraftID: strings.Repeat("a", 32), Draft: json.RawMessage(`{"subject":"original"}`)}
	result, e := draftInDir(dir, q)
	if e != nil {
		t.Fatal(e)
	}
	stale := q
	stale.Draft = json.RawMessage(`{"subject":"stale overwrite"}`)
	if _, e = draftInDir(dir, stale); e == nil {
		t.Fatal("accepted stale overwrite")
	}
	q.Op = "draft-get"
	read, e := draftInDir(dir, q)
	if e != nil {
		t.Fatal(e)
	}
	if string(read["draft"].(json.RawMessage)) != `{"subject":"original"}` {
		t.Fatal("lost saved draft")
	}
	q.Op = "draft-delete"
	if _, e = draftInDir(dir, q); e == nil {
		t.Fatal("accepted stale deletion")
	}
	q.Revision = result["revision"].(string)
	if _, e = draftInDir(dir, q); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(dir, q.DraftID+".json")); !os.IsNotExist(e) {
		t.Fatal("draft was not removed")
	}
}

func TestAttachmentPathsCannotEscapeStagingDirectory(t *testing.T) {
	items := []Attachment{{Name: "../../report.txt", Data: base64.StdEncoding.EncodeToString([]byte("test"))}, {Name: "report.txt", Data: base64.StdEncoding.EncodeToString([]byte("other"))}}
	dir, e := stageAttachments(items)
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(dir)
	for _, a := range items {
		rel, e := filepath.Rel(dir, a.Path)
		if e != nil || strings.HasPrefix(rel, "..") {
			t.Fatal("escaped attachment path")
		}
		st, e := os.Stat(a.Path)
		if e != nil || st.Mode().Perm() != 0600 {
			t.Fatal("unsafe attachment permissions")
		}
	}
	if items[0].Path == items[1].Path {
		t.Fatal("attachment collision")
	}
}

func TestInlineAttachmentsDecoded(t *testing.T) {
	raw := "Content-Type: multipart/related; boundary=x\r\n\r\n--x\r\nContent-Type: text/html\r\n\r\n<img src=\"cid:test\">\r\n--x\r\nContent-Type: image/png; name=test.png\r\nContent-ID: <test>\r\nContent-Transfer-Encoding: base64\r\n\r\nYWJj\r\n--x--\r\n"
	m, e := mail.ReadMessage(strings.NewReader(raw))
	if e != nil {
		t.Fatal(e)
	}
	var out []Attachment
	if e = extractAttachments(m.Header, m.Body, 0, &out); e != nil {
		t.Fatal(e)
	}
	if len(out) != 1 || out[0].CID != "test" || out[0].Data != "YWJj" {
		t.Fatal("inline image lost")
	}
}

func TestRequestRejectsHeaderInjectionAndTrailingData(t *testing.T) {
	good := Request{Op: "send", Mode: "new", Sender: "me@example.com", Bcc: []string{"you@example.com"}, Subject: "Hello"}
	if err := Validate(good); err != nil {
		t.Fatal(err)
	}
	bad := good
	bad.Subject = "Hello\r\nBcc: hidden@example.com"
	if Validate(bad) == nil {
		t.Fatal("accepted subject injection")
	}
	bad = good
	bad.Bcc = []string{"you@example.com\r\nCc: hidden@example.com"}
	if Validate(bad) == nil {
		t.Fatal("accepted recipient injection")
	}
	for _, raw := range []string{`{"op":"accounts"} {"op":"send"}`, `{"op":"accounts","unexpected":true}`, strings.Repeat("x", MaxRequest+1)} {
		if _, err := Decode(strings.NewReader(raw)); err == nil {
			t.Fatal("accepted malformed request")
		}
	}
	q, err := Decode(strings.NewReader(`{"op":"read","localId":42,"resolvedAccount":"untrusted"}`))
	if err != nil || q.LocalID != 0 || q.Account != "" {
		t.Fatal("caller supplied resolution was not discarded")
	}
}

func TestMultipartDoesNotDisplayAttachmentAsBody(t *testing.T) {
	raw := "Content-Type: multipart/mixed; boundary=x\r\n\r\n--x\r\nContent-Type: text/plain\r\nContent-Disposition: attachment; filename=secret.txt\r\n\r\nNot the body\r\n--x\r\nContent-Type: multipart/alternative; boundary=y\r\n\r\n--y\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: base64\r\n\r\nSGVsbG8=\r\n--y\r\nContent-Type: text/html; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n<p>Hello =3D world</p>\r\n--y--\r\n--x--\r\n"
	m, e := mail.ReadMessage(strings.NewReader(raw))
	if e != nil {
		t.Fatal(e)
	}
	plain, rich := parts(m.Header, m.Body, 0)
	if plain != "Hello" || rich != "<p>Hello = world</p>" {
		t.Fatalf("wrong MIME alternatives: %q %q", plain, rich)
	}
}
