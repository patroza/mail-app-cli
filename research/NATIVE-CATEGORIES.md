# Native category reader (experimental)

Validated on macOS Tahoe 26.6.2, build 25G83, Intel, September 6 2026.

`mail-app-cli categories native --limit 100` reads recent physical Inbox rows
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

## Why this is not the notification backend yet

Output explicitly has `experimental:true` and `verified:false`. It is a metadata
inspection result, not a drop-in Primary feed. Existing UI Primary remains the
default, and the notifier rejects unverified results.

In a paired sample, nine of 76 UI-matched Primary rows had native persisted
categories outside Primary with high-impact false. All nine UI rows were marked
time-sensitive by the AX reader. Stale UI state, AX interpretation, and other
service behavior have not been distinguished. Do not claim exact UI parity.
The current AX reader detects the time-sensitive identifier's presence, which
also needs visibility validation. Physical `/INBOX` scope does not yet establish
parity with every account's virtual/label mailbox representation.

Attempted read-only `EMDaemonInterface`/`EMMessageRepository` queries were rejected
by maild for a missing entitlement, through both direct and ordinary initialization.
No entitlement bypass was attempted. Persisted-state resolution remains available.

Next validation should obtain fresh native AX row identities/visibility and
compare a refreshed, unselected Primary view against a coherent database snapshot.
Only after unexplained differences are resolved should the native backend produce
the existing stable mailbox-URL/remote-UID IDs and replace polling in production.

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
