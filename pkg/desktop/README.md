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

### Opt-in expanded browsing

`mailboxes` lists populated server-backed mailboxes (`id`, `name`, `accountId`,
`kind`). `mail-list` accepts `mailbox` (`inbox`, `all`, or returned ID), `category`
(`all`, `Primary`, `Transactions`, `Updates`, `Promotions`; categories only on
Inbox), `includeTimeSensitive` (default true), `unreadOnly`, `limit` (default100,
maximum200), and an opaque `cursor`. Results include `messages` and `nextCursor`.
Messages expose `underlyingCategory`, `timeSensitive`, `primary`, `primaryReason`
and mailbox identity. The client can exclude time-sensitive promotion into
Primary without changing persisted Apple classifications.

All Mail excludes recognized Junk, Trash and Drafts names; unknown folder roles
are included. Empty and local-only folders are omitted. Pagination uses a
request-bound offset; concurrent mailbox changes require refreshing. This is
not a full-body search API. Legacy `list` remains unchanged and is the
rollback route. Mailbox-based message IDs still change on external moves.

Validated on Tahoe: 100-row category/all-mail requests below one second in the
sample, 200 distinct rows across two pages, cached Sent read and an unsent
Sent-folder reply preview. No test email was sent. Apple Silicon cross-build
passes; runtime there is not yet tested.

`mail-list` also accepts `query`: a literal, case/diacritic-insensitive substring
across subject, sender name and sender address, evaluated before category
pagination over persisted mail metadata. Query is part of the cursor scope.
SQL parameters bind search text; wildcard/SQL syntax has no special meaning.
Message-body, recipient and attachment-content search are not included. Reads
remain on demand. A live probe found a message outside the first100 loaded rows;
search, no-match and subsequent-page samples completed around0.5seconds.


### Conversations (optional client view)

`mail-list` (and native `list` in this build) adds `threadId` and `accountId`.
Thread IDs hash Apple's persisted `messages.conversation_id` together with the
account, so unrelated accounts and repeated subject lines cannot merge. Missing
conversation IDs become singleton messages. IDs are local to this Mac's Mail
index and should not be persisted across rebuilding that index.

`{"op":"thread-list","id":"<message-id>","limit":100,"cursor":""}` returns
`messages` with the same shape as `mail-list`, oldest first, plus `nextCursor`.
The anchor is resolved server-side; clients cannot select arbitrary account or
conversation SQL identifiers. Limit is 1–200 (default 100). Pagination is scoped
to the resolved anchor/account mailbox set. It includes indexed same-account
Sent and other folders across the conversation, regardless of current category
or unread filters, excluding recognized Junk, Trash, and Drafts. Unknown custom
folder roles are included. Cached messages may exist in multiple folders.
Opening/listing a conversation does not mark any member read. The client must
mark individual actually viewed messages through the existing `mark` operation.
Bodies and attachments continue to load on demand using each member's `id`.
Mailbox changes during offset pagination require refreshing the conversation.
