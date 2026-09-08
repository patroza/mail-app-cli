package desktop

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Disposable cache; every identity still resolves against the live index.
func deferReadAttachments(result map[string]any) {
	m, ok := result["message"].(map[string]any)
	if !ok {
		return
	}
	raw, e := json.Marshal(m["attachments"])
	if e != nil {
		return
	}
	var items []map[string]any
	if json.Unmarshal(raw, &items) != nil {
		return
	}
	html, _ := m["html"].(string)
	for _, a := range items {
		cid, _ := a["cid"].(string)
		if a["inline"] == true || (cid != "" && strings.Contains(html, "cid:"+cid)) {
			continue
		}
		if data, ok := a["data"].(string); ok && data != "" {
			delete(a, "data")
			a["deferred"] = true
		}
	}
	m["attachments"] = items
}

func readCachePath(id, kind string) string {
	if os.Getenv("PRIMARY_MAIL_CACHE_DISABLE") == "1" {
		return ""
	}
	b, e := hex.DecodeString(id)
	if e != nil || len(b) != 32 {
		return ""
	}
	root, e := os.UserCacheDir()
	if e != nil {
		return ""
	}
	root = filepath.Join(root, "PrimaryMailBridge", "reads-v1")
	if os.MkdirAll(root, 0700) != nil {
		return ""
	}
	if st, e := os.Lstat(root); e != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return ""
	}
	os.Chmod(root, 0700)
	return filepath.Join(root, id+"."+kind)
}
func loadReadCache(id, kind string, out any) bool {
	p := readCachePath(id, kind)
	if p == "" {
		return false
	}
	st, e := os.Lstat(p)
	if e != nil || !st.Mode().IsRegular() || st.Size() > 32<<20 {
		return false
	}
	raw, e := os.ReadFile(p)
	return e == nil && json.Unmarshal(raw, out) == nil
}
func writeReadCache(id, kind string, value any) {
	p := readCachePath(id, kind)
	if p == "" {
		return
	}
	raw, e := json.Marshal(value)
	if e != nil || len(raw) > 32<<20 {
		return
	}
	f, e := os.CreateTemp(filepath.Dir(p), ".cache-")
	if e != nil {
		return
	}
	defer os.Remove(f.Name())
	_, e = f.Write(raw)
	closeErr := f.Close()
	if e != nil || closeErr != nil {
		return
	}
	if os.Rename(f.Name(), p) != nil {
		return
	}
	entries, _ := os.ReadDir(filepath.Dir(p))
	files := []fs.FileInfo{}
	var total int64
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if st, e := entry.Info(); e == nil && st.Mode().IsRegular() {
			files = append(files, st)
			total += st.Size()
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].ModTime().Before(files[j].ModTime()) })
	for i, st := range files {
		if len(files)-i <= 512 && total <= 256<<20 {
			break
		}
		if os.Remove(filepath.Join(filepath.Dir(p), st.Name())) == nil {
			total -= st.Size()
		}
	}
}
func cachedLocator(id string) int64 { var row int64; loadReadCache(id, "locator", &row); return row }

type sourceEntry struct {
	Path, Stamp string
	Checked     time.Time
	Result      map[string]any
}

func sourceStamp(path string, id int64) string {
	st, e := os.Stat(path)
	if e != nil || !st.Mode().IsRegular() {
		return ""
	}
	hash := sha256.New()
	fmt.Fprintf(hash, "%s:%d:%d", path, st.Size(), st.ModTime().UnixNano())
	attachments := filepath.Join(filepath.Dir(filepath.Dir(path)), "Attachments", strconv.FormatInt(id, 10))
	e = filepath.WalkDir(attachments, func(p string, d fs.DirEntry, e error) error {
		if os.IsNotExist(e) && p == attachments {
			return nil
		}
		if e != nil {
			return e
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		fmt.Fprintf(hash, "|%s:%d:%d", p, info.Size(), info.ModTime().UnixNano())
		return nil
	})
	if e != nil {
		return ""
	}
	return fmt.Sprintf("%x", hash.Sum(nil))
}
func cachedLocalMessage(q Request, root string) map[string]any {
	var entry sourceEntry
	if !loadReadCache(q.ID, "body", &entry) || time.Since(entry.Checked) > 120*time.Second {
		return nil
	}
	real, e := filepath.EvalSymlinks(entry.Path)
	if e != nil {
		return nil
	}
	rel, e := filepath.Rel(root, real)
	if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return nil
	}
	if entry.Stamp == "" || entry.Stamp != sourceStamp(real, q.LocalID) {
		return nil
	}
	m, ok := entry.Result["message"].(map[string]any)
	if !ok || m["id"] != q.ID {
		return nil
	}
	m["read"] = q.StoredRead
	m["date"] = time.Unix(q.Received, 0).UTC().Format(time.RFC3339)
	entry.Result["backend"] = "local-parsed-cache"
	return entry.Result
}
