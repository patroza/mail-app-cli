package desktop

import (
	"strings"
	"testing"
)

func TestInlinePositions(t *testing.T) {
	q := Request{Body: "😀before\nafter", Attachments: []Attachment{
		{Inline: true, Offset: 8, MIME: "image/png", Path: "/tmp/first.png"},
		{Inline: true, Offset: 0, MIME: "image/jpeg", Path: "/tmp/start.jpg"},
		{Inline: true, Offset: 8, MIME: "image/png", Path: "/tmp/second.png"},
		{Path: "/tmp/file.pdf"},
	}}
	if err := validateInline(q); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	if writeAttachments(&b, q) != 3 {
		t.Fatal("missing inline images")
	}
	s := b.String()
	if !strings.Contains(s, `"/tmp/first.png")} at before character 10`) {
		t.Fatal("lost non-BMP offset")
	}
	if strings.Index(s, "/tmp/second.png") > strings.Index(s, "/tmp/first.png") {
		t.Fatal("same-position order is reversed")
	}
	for _, position := range []string{"character 1 of", "character 11 of", "character 12 of"} {
		if !strings.Contains(s, position) {
			t.Fatalf("missing verification %s", position)
		}
	}
	q.Body = ""
	q.Attachments = []Attachment{{Inline: true, Offset: 0, MIME: "image/png"}}
	if err := validateInline(q); err != nil || inlineCharacter(q.Body, 0) != 1 {
		t.Fatal("empty body rejected")
	}
}

func TestInvalidInlineAttachments(t *testing.T) {
	for _, a := range []Attachment{
		{Inline: true, Offset: -1, MIME: "image/png"},
		{Inline: true, Offset: 2, MIME: "image/png"},
		{Inline: true, MIME: "image/svg+xml"},
		{Inline: true, MIME: "application/pdf"},
	} {
		if validateInline(Request{Body: "😀", Attachments: []Attachment{a}}) == nil {
			t.Fatalf("accepted invalid inline attachment: %+v", a)
		}
	}
}
