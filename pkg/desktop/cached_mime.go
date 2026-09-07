package desktop

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type cachedContent struct {
	Plain       string
	HTML        string
	Attachments []Attachment
	Missing     []string
	Bytes       int
}

// Apple stores decoded payloads removed from partial .emlx files in a sibling
// Attachments/messageID/MIME-part-number directory. Read those directly instead
// of asking Mail to synchronously reconstruct and download the entire message.
func cachedMIME(h mail.Header, r io.Reader, root, index string, depth int, out *cachedContent) {
	if depth > 15 {
		out.Missing = append(out.Missing, "nested MIME content")
		return
	}
	kind, params, e := mime.ParseMediaType(h.Get("Content-Type"))
	if e != nil {
		kind = "text/plain"
	}
	if strings.HasPrefix(kind, "multipart/") {
		mr := multipart.NewReader(r, params["boundary"])
		for n := 1; n <= 200; n++ {
			p, e := mr.NextRawPart()
			if e == io.EOF {
				return
			}
			if e != nil {
				out.Missing = append(out.Missing, "message content")
				return
			}
			child := strconv.Itoa(n)
			if index != "" {
				child = index + "." + child
			}
			cachedMIME(mail.Header(p.Header), p, root, child, depth+1, out)
		}
		out.Missing = append(out.Missing, "additional MIME parts")
		return
	}
	if index == "" {
		index = "1"
	}
	disposition, disp, _ := mime.ParseMediaType(h.Get("Content-Disposition"))
	name := disp["filename"]
	if name == "" {
		name = params["name"]
	}
	if decoded, e := new(mime.WordDecoder).DecodeHeader(name); e == nil {
		name = decoded
	}
	cid := strings.Trim(h.Get("Content-ID"), "<>")
	isBody := disposition != "attachment" && name == "" && (kind == "text/plain" || kind == "text/html")
	if name == "" {
		if isBody {
			name = "message body"
		} else {
			name = fmt.Sprintf("attachment-%s", index)
		}
	}
	raw, e := io.ReadAll(io.LimitReader(r, (32<<20)+1))
	if e != nil || len(raw) > 32<<20 {
		out.Missing = append(out.Missing, name+" (too large)")
		return
	}
	external := false
	missing := false
	declared, _ := strconv.ParseInt(h.Get("X-Apple-Content-Length"), 10, 64)
	if declared > 0 && len(bytes.TrimSpace(raw)) == 0 {
		files, _ := filepath.Glob(filepath.Join(root, index, "*"))
		regular := []string{}
		for _, f := range files {
			if st, e := os.Lstat(f); e == nil && st.Mode().IsRegular() {
				regular = append(regular, f)
			}
		}
		if len(regular) == 1 {
			f, e := os.Open(regular[0])
			if e == nil {
				raw, e = io.ReadAll(io.LimitReader(f, (32<<20)+1))
				f.Close()
			}
			if e == nil && len(raw) > 0 && len(raw) <= 32<<20 {
				external = true
			} else {
				missing = true
			}
		} else {
			missing = true
		}
	}
	if !external && !missing {
		var decoded io.Reader = bytes.NewReader(raw)
		switch strings.ToLower(h.Get("Content-Transfer-Encoding")) {
		case "base64":
			decoded = base64.NewDecoder(base64.StdEncoding, decoded)
		case "quoted-printable":
			decoded = quotedprintable.NewReader(decoded)
		}
		raw, e = io.ReadAll(io.LimitReader(decoded, (32<<20)+1))
		if e != nil {
			missing = true
		}
	}
	out.Bytes += len(raw)
	if len(raw) > 32<<20 || out.Bytes > 32<<20 {
		missing = true
	}
	if missing {
		out.Missing = append(out.Missing, name)
		if !isBody {
			out.Attachments = append(out.Attachments, Attachment{Name: name, MIME: kind, CID: cid, Unavailable: true})
		}
		return
	}
	if isBody {
		// Reuse charset conversion, with transfer encoding removed: data is decoded.
		clean := mail.Header{"Content-Type": []string{h.Get("Content-Type")}}
		plain, rich := parts(clean, bytes.NewReader(raw), 0)
		if len(raw) > 0 && plain == "" && rich == "" {
			out.Missing = append(out.Missing, name+" (unsupported encoding)")
		}
		if out.Plain == "" {
			out.Plain = plain
		}
		if out.HTML == "" {
			out.HTML = rich
		}
	} else {
		out.Attachments = append(out.Attachments, Attachment{Name: name, MIME: kind, CID: cid, Data: base64.StdEncoding.EncodeToString(raw)})
	}
}
