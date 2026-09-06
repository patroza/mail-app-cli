# Native category reader

Validated on macOS Tahoe 26.6.2, build 25G83, Intel, September 6 2026.

`mail-app-cli categories primary --limit 100` is the default native Primary feed.
`categories native --limit 100` additionally inspects all categories and raw
metadata (that diagnostic output retains `verified:false`). Both read Inbox rows
using SQLite READONLY, wraps the live statement in Apple's `EFSQLRow`, and calls
`EDCategoryPersistence.categoryForResultRow:`. Membership uses Apple's own
`EMMessageListItemPredicates.predicateForPrimaryMessages`, not a keyword heuristic.
It does not launch Mail, use Accessibility/AppleScript, select messages, or write
to the database. The logged-in account/background services still supply sync.

The command needs Full Disk Access for the invoking execution context and Apple's
Command Line Tools to compile its embedded helper on first use. Existing SSH
access worked without a new permission prompt. macOS private selectors/schema
can change; unavailable selectors, schema errors and unknown category types fail.
The source has no Intel-specific code, but Apple Silicon/newer macOS are untested.

## Confirmed native behavior

- Primary predicate: `category.type == 0 OR category.isHighImpact == 1`.
- Type 0 is Default/Primary, 1 Transactions, 2 Updates, 3 Promotions.
- Expanded category predicates prefer sender category over model category;
  missing sender and missing model category fall back to Primary.
- The native column projection maps `business_addresses.category` to the alias
  `business_category`, alongside `model_category`, `model_subcategory`,
  `model_high_impact`, and `category_is_temporary` from global-message data.
  `business_categories` is displayed separately for diagnostics; it is not
  substituted for the native projected sender override.
- `research/native-category-mapper.m` reproduces that projection without opening
  any message database. Compile it with `clang -fobjc-arc -framework Foundation`.

## Validation against Mail and activation

The first paired sample had nine disagreements: UI rows showed Primary/time-sensitive,
while Apple's persisted resolver did not. After normal Mail quit/reopen with no
message selected, all nine disappeared from the checked Primary list. All tracked
read states were preserved. This establishes stale UI state in that sample.

All 75 uniquely identified rows in the refreshed UI sample resolved native Primary,
with matching time-sensitive flags. Reverse comparison explained the remaining
raw UI rows: ambiguous duplicate candidates, a `RE: ` prefix omitted by the old
matcher, and a `REMIND ME` date label. No unexplained category mismatch remained
in this bounded comparison. The native feed includes subject prefixes, uses Mail's
`display_date` ordering (received date fallback), and avoids text/date matching.
Its 100-message cutoff can differ from 100 UI rows because the latter dropped
ambiguous results. It emits stable mailbox-URL/remote-UID hashes, preserves boolean
unread state, and deduplicates identities for the existing notifier contract.

Default Primary reads now use the native backend. `--backend ax` remains an
explicit diagnostic fallback; failure never silently switches back to UI.
The metadata inspection command remains a separate all-category diagnostic.

Attempted read-only `EMDaemonInterface`/`EMMessageRepository` queries were rejected
by maild for a missing entitlement, through both direct and ordinary initialization.
No entitlement bypass was attempted. The deployed resolver uses read-only persisted
state and needs no maild entitlement.

Scope: physical Inbox rows; this has been validated with this iCloud account, not
all providers' virtual/label mailboxes. It is not a complete unread count or proof
of cloud freshness. Apple can change private selectors/schema, and newer OS builds
must be revalidated. Writes still use the existing UI automation; this work replaces
routine category **reads**, not sender-rule writes.

## Validation

Go tests: `go test ./...`.

On macOS, from `pkg/categories`:

```sh
clang -x objective-c -framework Foundation -lsqlite3 native_test.m.txt -o /tmp/mail-native-test
/tmp/mail-native-test
```

Five synthetic SELECT-only fixtures exercise the real Apple resolver: null
fallback, two sender overrides, time-sensitive overlap, and a disputed UI case.
No real database is opened by the fixture test. Live reads returned 100 messages
through existing SSH wrappers on two Linux clients; unread values remain booleans.

Useful discovery references (runtime behavior above was verified on Tahoe):

- https://github.com/qingralf/iOS18-Runtime-Headers/blob/main/PrivateFrameworks/Email.framework/EMMessageListItemPredicates.h
- https://github.com/qingralf/iOS18-Runtime-Headers/blob/main/PrivateFrameworks/EmailDaemon.framework/EDCategoryPersistence.h
- https://support.apple.com/en-bh/guide/mail/mlhl64d76621/mac
