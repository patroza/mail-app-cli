package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

func appleString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`, "\t", `\t`).Replace(s) + `"`
}

func validateInline(q Request) error {
	for _, a := range q.Attachments {
		if !a.Inline {
			continue
		}
		if a.Offset < 0 || a.Offset > utf8.RuneCountInString(q.Body) {
			return errors.New("inline image position is outside the message body")
		}
		switch strings.ToLower(a.MIME) {
		case "image/png", "image/jpeg", "image/gif", "image/webp":
		default:
			return errors.New("inline attachments must be PNG, JPEG, GIF or WebP images")
		}
	}
	return nil
}

// Mail's text scripting indexes UTF-16 units; the Linux editor indexes code points.
func inlineCharacter(body string, offset int) int {
	return len(utf16.Encode([]rune(body)[:offset])) + 1
}

func writeAttachments(b *strings.Builder, q Request) int {
	b.WriteString("set stage to \"attachments\"\ntell d\n")
	inline := []Attachment{}
	for _, a := range q.Attachments {
		if a.Inline {
			inline = append(inline, a)
			continue
		}
		fmt.Fprintf(b, "make new attachment with properties {file name:(POSIX file %s)} at after last paragraph\n", appleString(a.Path))
	}
	b.WriteString("end tell\ntell content of d\n")
	sort.SliceStable(inline, func(i, j int) bool { return inline[i].Offset < inline[j].Offset })
	// Reverse insertion preserves original offsets and ordering for identical positions.
	for i := len(inline) - 1; i >= 0; i-- {
		a := inline[i]
		fmt.Fprintf(b, "make new attachment with properties {file name:(POSIX file %s)} at before character %d\n", appleString(a.Path), inlineCharacter(q.Body, a.Offset))
	}
	b.WriteString("end tell\n")
	for i, a := range inline {
		fmt.Fprintf(b, "if (character %d of content of d as text) is not \"￼\" then error \"inline image position mismatch\"\n", inlineCharacter(q.Body, a.Offset)+i)
	}
	return len(inline)
}
func compose(ctx context.Context, q Request) (map[string]any, error) {
	accounts, e := Execute(ctx, Request{Op: "accounts"})
	if e != nil {
		return nil, e
	}
	allowed := false
	if rows, ok := accounts["accounts"].([]any); ok {
		for _, row := range rows {
			a, _ := row.(map[string]any)
			if a["accountId"] == q.AccountID && a["address"] == q.Sender {
				allowed = true
			}
		}
	}
	if !allowed {
		return nil, errors.New("the selected sending address is not available on the Mac")
	}
	unlock, e := automationLock(ctx)
	if e != nil {
		return nil, e
	}
	defer unlock()
	dir, e := stageAttachments(q.Attachments)
	if e != nil {
		return nil, e
	}
	defer os.RemoveAll(dir)
	var b strings.Builder
	b.WriteString("set stage to \"create\"\nset d to missing value\ntell application \"Mail\"\ntry\n")
	if q.Mode == "reply" {
		fmt.Fprintf(&b, "set targetAccount to first account whose id is %s\nset targetMailbox to mailbox \"INBOX\" of targetAccount\nset original to first message of targetMailbox whose id is %d\nset d to reply original without opening window\n", appleString(q.Account), q.LocalID)
	} else {
		b.WriteString("set d to make new outgoing message with properties {visible:false}\n")
	}
	fmt.Fprintf(&b, "set stage to \"content\"\nset subject of d to %s\nset sender of d to %s\nset content of d to %s\n", appleString(q.Subject), appleString(q.Sender), appleString(q.Body))
	b.WriteString("set stage to \"recipients\"\n")
	for _, spec := range []struct {
		kind string
		rows []string
	}{{"to", q.To}, {"cc", q.Cc}, {"bcc", q.Bcc}} {
		fmt.Fprintf(&b, "repeat with r in (get %s recipients of d)\ndelete r\nend repeat\n", spec.kind)
		for _, address := range spec.rows {
			fmt.Fprintf(&b, "make new %s recipient at end of %s recipients of d with properties {address:%s}\n", spec.kind, spec.kind, appleString(address))
		}
	}
	inlineCount := writeAttachments(&b, q)
	b.WriteString("set attachmentCount to count attachments of content of d\n")
	fmt.Fprintf(&b, "if attachmentCount is not %d then error \"attachment count mismatch\"\n", len(q.Attachments))
	if q.Op == "preview" {
		result, _ := json.Marshal(map[string]any{"ok": true, "preview": true, "to": len(q.To), "cc": len(q.Cc), "bcc": len(q.Bcc), "attachments": len(q.Attachments), "inlinePositionsVerified": inlineCount})
		fmt.Fprintf(&b, "set stage to \"cleanup\"\nclose d saving no\nreturn %s\n", appleString(string(result)))
	} else {
		b.WriteString("set stage to \"send\"\nif not (send d) then error \"Mail did not accept send\"\nreturn \"{\\\"ok\\\":true,\\\"accepted\\\":true}\"\n")
	}
	b.WriteString("on error errText number errNum\n")
	if q.Op == "preview" {
		b.WriteString("try\nif d is not missing value then close d saving no\nend try\n")
	}
	b.WriteString("return \"{\\\"ok\\\":false,\\\"stage\\\":\\\"\" & stage & \"\\\",\\\"code\\\":\" & errNum & \",\\\"error\\\":\\\"Mail could not confirm the operation. Check Drafts, Outbox and Sent on the Mac before retrying.\\\"}\"\nend try\nend tell\n")
	cmd := exec.CommandContext(ctx, "/usr/bin/osascript", "-")
	cmd.Stdin = strings.NewReader(b.String())
	raw, e := cmd.Output()
	if e != nil {
		return nil, errors.New("Mail automation could not confirm the operation")
	}
	var result map[string]any
	if json.Unmarshal(raw, &result) != nil {
		return nil, errors.New("invalid compose response")
	}
	return result, nil
}
