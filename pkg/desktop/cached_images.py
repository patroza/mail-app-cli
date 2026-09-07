"""Read only exact URLs from the selected message in Mail's existing cache.

Never fetch, mutate the cache, or accept filesystem paths from the caller.
"""
import base64
import html
import json
import os
from pathlib import Path
import re
import sqlite3
import stat
import sys
from urllib.parse import urlsplit

MAX_IMAGE = 8 << 20
MAX_TOTAL = 20 << 20


def cached_images(source, base):
    images = {}
    urls = []
    for match in re.finditer(r'https?://[^\s<>\x00-\x20"\'\\)]+', source, re.I):
        url = html.unescape(match.group())
        try:
            parsed = urlsplit(url)
            if len(url) > 8192 or not parsed.hostname or parsed.username or parsed.password:
                continue
        except ValueError:
            continue
        if url not in urls:
            urls.append(url)
        if len(urls) == 64:
            break
    if not urls:
        return images
    database = base / 'Cache.db'
    if database.is_symlink() or not database.is_file():
        return images
    connection = sqlite3.connect(database.as_uri() + '?mode=ro', uri=True, timeout=1)
    connection.execute('PRAGMA query_only=ON')
    total = 0
    try:
        for url in urls:
            rows = connection.execute('''SELECT d.isDataOnFS, d.receiver_data
                FROM cfurl_cache_response r JOIN cfurl_cache_receiver_data d
                ON r.entry_ID=d.entry_ID WHERE r.request_key=?
                AND length(d.receiver_data)<=? ORDER BY r.time_stamp DESC LIMIT 1''',
                (url, MAX_IMAGE)).fetchall()
            for external, value in rows:
                if external:
                    if not isinstance(value, str) or not re.fullmatch(r'[A-Za-z0-9_-]+', value):
                        continue
                    directory = base / 'fsCachedData'
                    if directory.is_symlink() or not directory.is_dir():
                        continue
                    try:
                        descriptor = os.open(directory / value, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
                        with os.fdopen(descriptor, 'rb') as file:
                            info = os.fstat(file.fileno())
                            if not stat.S_ISREG(info.st_mode) or info.st_size > MAX_IMAGE:
                                continue
                            data = file.read(MAX_IMAGE + 1)
                    except OSError:
                        continue
                elif isinstance(value, bytes):
                    data = value
                else:
                    continue
                # Final image format/dimension validation occurs in the Go caller.
                if not data.startswith((b'\x89PNG\r\n\x1a\n', b'\xff\xd8\xff', b'GIF87a', b'GIF89a')):
                    continue
                if len(data) > MAX_IMAGE or total + len(data) > MAX_TOTAL:
                    continue
                total += len(data)
                images[url] = base64.b64encode(data).decode('ascii')
    finally:
        connection.close()
    return images


if __name__ == '__main__':
    try:
        source = json.load(sys.stdin)
        base = Path.home() / 'Library/Containers/com.apple.mail/Data/Library/Caches/com.apple.mail'
        result = cached_images(source, base)
    except (OSError, ValueError, sqlite3.Error):
        result = {}
    json.dump(result, sys.stdout)
