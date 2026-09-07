package desktop

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"strings"
	"testing"
)

func TestCachedRasterValidation(t *testing.T) {
	var body bytes.Buffer
	if err := png.Encode(&body, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString(body.Bytes())
	if !strings.HasPrefix(rasterURI(encoded), "data:image/png;base64,") {
		t.Fatal("valid PNG rejected")
	}
	for _, bad := range []string{"invalid", base64.StdEncoding.EncodeToString([]byte("<svg/>")), base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\ninvalid"))} {
		if rasterURI(bad) != "" {
			t.Fatal("non-image accepted")
		}
	}
	body.Reset()
	if err := png.Encode(&body, image.NewRGBA(image.Rect(0, 0, 16385, 1))); err != nil {
		t.Fatal(err)
	}
	if rasterURI(base64.StdEncoding.EncodeToString(body.Bytes())) != "" {
		t.Fatal("oversized image accepted")
	}
	if Validate(Request{Op: "cached-images", ID: strings.Repeat("a", 64)}) != nil {
		t.Fatal("valid cached-image request rejected")
	}
	if Validate(Request{Op: "cached-images", ID: "../Cache.db"}) == nil {
		t.Fatal("invalid identity accepted")
	}
}
