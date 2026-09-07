# Existing Mail image cache

The stdin desktop protocol accepts `{"op":"cached-images","id":"<message-id>"}`.
It resolves the actual inbox message, derives a bounded list of remote URLs from
its locally cached HTML, and looks up exact URLs in Mail's read-only CFURL cache.
The result's `images` maps original URLs to validated PNG/JPEG/GIF data URIs.
Missing entries return no image; this operation never downloads remote content.

This uses a private macOS cache layout and is best-effort. Cache misses, unsupported
formats, unavailable Python/cache, and format changes produce an empty mapping.
Existing cached images do not establish that Mail Privacy Protection was enabled
when Apple Mail downloaded them. No relay/privacy guarantee is made.

Limits: 64 candidate URLs, 8 MiB per image, 20 MiB total; 16,384 pixels per
dimension and 40 million pixels total. Cache paths cannot be supplied by the
caller; external cache names cannot contain path separators and symlinks are
rejected. Database queries are parameterized and read-only. Font, SVG, HTML and
other executable or non-raster cache entries are excluded.

Tests: `go test ./pkg/desktop` and
`python3 -m unittest discover -s pkg/desktop -p 'test_cached_images.py'`.
