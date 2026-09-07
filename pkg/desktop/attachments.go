package desktop

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
)

type Attachment struct {
	Inline      bool   `json:"inline,omitempty"`
	Offset      int    `json:"offset,omitempty"` // Unicode code points into the plain body, before insertion.
	Unavailable bool   `json:"unavailable,omitempty"`
	Name        string `json:"name"`
	MIME        string `json:"mime"`
	CID         string `json:"cid,omitempty"`
	Data        string `json:"data"`
	Path        string `json:"path,omitempty"`
}

func extractAttachments(header mail.Header, r io.Reader, depth int, out *[]Attachment) error {
	if depth > 15 {
		return errors.New("message MIME nesting exceeds limit")
	}
	kind, params, e := mime.ParseMediaType(header.Get("Content-Type"))
	if e != nil {
		kind = "application/octet-stream"
	}
	disposition, disp, _ := mime.ParseMediaType(header.Get("Content-Disposition"))
	if strings.HasPrefix(kind, "multipart/") {
		mr := multipart.NewReader(r, params["boundary"])
		for i := 0; i < 200; i++ {
			p, e := mr.NextPart()
			if e == io.EOF {
				return nil
			}
			if e != nil {
				return errors.New("invalid attachment MIME structure")
			}
			if e = extractAttachments(mail.Header(p.Header), p, depth+1, out); e != nil {
				return e
			}
		}
		return errors.New("too many MIME parts")
	}
	name := disp["filename"]
	if name == "" {
		name = params["name"]
	}
	cid := strings.Trim(header.Get("Content-ID"), "<>")
	if disposition != "attachment" && name == "" && (kind == "text/plain" || kind == "text/html") {
		return nil
	}
	switch strings.ToLower(header.Get("Content-Transfer-Encoding")) {
	case "base64":
		r = base64.NewDecoder(base64.StdEncoding, r)
	case "quoted-printable":
		r = quotedprintable.NewReader(r)
	}
	data, e := io.ReadAll(io.LimitReader(r, (20<<20)+1))
	if e != nil || len(data) > 20<<20 {
		return errors.New("attachment cannot be decoded or exceeds 20 MiB")
	}
	if name == "" {
		name = fmt.Sprintf("attachment-%d", len(*out)+1)
	}
	if decoded, e := new(mime.WordDecoder).DecodeHeader(name); e == nil {
		name = decoded
	}
	*out = append(*out, Attachment{Name: name, MIME: kind, CID: cid, Data: base64.StdEncoding.EncodeToString(data)})
	return nil
}

func stageAttachments(items []Attachment) (string, error) {
	if len(items) > 100 {
		return "", errors.New("too many attachments")
	}
	dir, e := os.MkdirTemp("", "primary-mail-attachments-")
	if e != nil {
		return "", errors.New("cannot stage attachments on Mac")
	}
	total := 0
	for i := range items {
		if items[i].Unavailable {
			os.RemoveAll(dir)
			return "", errors.New("an attachment is not downloaded yet")
		}
		data, e := base64.StdEncoding.DecodeString(items[i].Data)
		total += len(data)
		if e != nil || total > 20<<20 {
			os.RemoveAll(dir)
			return "", errors.New("attachments exceed 20 MiB or contain invalid data")
		}
		name := filepath.Base(strings.ReplaceAll(items[i].Name, "\\", "/"))
		if name == "." || name == "/" || name == "" {
			name = "attachment"
		}
		// Per-attachment directories preserve names without collisions or path traversal.
		sub := filepath.Join(dir, fmt.Sprint(i))
		if e = os.Mkdir(sub, 0700); e != nil {
			os.RemoveAll(dir)
			return "", e
		}
		items[i].Path = filepath.Join(sub, name)
		if e = os.WriteFile(items[i].Path, data, 0600); e != nil {
			os.RemoveAll(dir)
			return "", e
		}
		items[i].Data = "" // Only a Mac-local file reference goes to Mail automation.
	}
	return dir, nil
}
