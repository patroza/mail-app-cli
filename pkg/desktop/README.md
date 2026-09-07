# Desktop mail protocol: attachments

Requests use `mail-app-cli desktop` with JSON on stdin. `send` and the unsent
`preview` operation accept `attachments`, each with `name`, `mime`, and base64
`data`. At most 100 attachments and 20 MiB decoded attachment bytes are accepted.
Files are staged privately on the Mac and removed after the operation. Client
file paths are never trusted. Forwarding unavailable attachment data is rejected.

For an image positioned inside the body, additionally set `inline: true` and
`offset` to a zero-based Unicode code-point boundary in the plain `body` **before
any images are inserted**. Offset zero inserts before the first character; body
length inserts at its end, including for an empty body. PNG, JPEG, GIF and WebP
are accepted. Identical offsets preserve attachment-array order. Ordinary
attachments are appended. The Linux client must remove editor placeholders from
the body before calculating these offsets.

Mail's native rich-text attachment API inserts each image at that position; the
bridge translates code-point offsets to Mail's UTF-16 indexes. It verifies native
attachment counts and each inline object position before sending. Mail controls
the eventual MIME encoding; this protocol does not accept arbitrary HTML or
client-chosen outgoing Content-IDs. Incoming CID images continue to use the
decoded MIME attachment data returned by `read`.

`preview` creates an invisible native outgoing message, verifies it, and closes it
without saving or sending. Its `inlinePositionsVerified` count records the native
position checks. Native Tahoe checks covered empty bodies, positions at the
start/middle/end, identical offsets, non-BMP characters and mixed ordinary/inline
attachments. These checks do not establish how every recipient mail client will
render the delivered MIME.
