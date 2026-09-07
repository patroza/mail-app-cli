package desktop

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os/exec"
	"time"
)

//go:embed cached_images.py
var cachedImagesScript string

func rasterURI(encoded string) string {
	if len(encoded) > (8<<20)*4/3+4 {
		return ""
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data) > 8<<20 {
		return ""
	}
	config, kind, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width < 1 || config.Height < 1 || config.Width > 16384 || config.Height > 16384 || int64(config.Width)*int64(config.Height) > 40_000_000 {
		return ""
	}
	if kind != "png" && kind != "jpeg" && kind != "gif" {
		return ""
	}
	return "data:image/" + kind + ";base64," + encoded
}

func cachedImages(ctx context.Context, q Request) (map[string]any, error) {
	local := localMessage(ctx, q)
	if local == nil {
		return nil, errors.New("Message is not cached on the Mac yet")
	}
	message := local["message"].(map[string]any)
	rich, _ := message["html"].(string)
	images := map[string]string{}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	payload, _ := json.Marshal(rich)
	command := exec.CommandContext(ctx, "/usr/bin/python3", "-c", cachedImagesScript)
	command.Stdin = bytes.NewReader(payload)
	raw, err := command.Output()
	if err == nil {
		var candidates map[string]string
		if json.Unmarshal(raw, &candidates) == nil {
			for url, encoded := range candidates {
				if uri := rasterURI(encoded); uri != "" {
					images[url] = uri
				}
			}
		}
	}
	return map[string]any{"ok": true, "images": images, "backend": "mail-url-cache", "privacy": "Existing Mail cache only; no network request. Cache provenance does not establish Mail Privacy Protection."}, nil
}
