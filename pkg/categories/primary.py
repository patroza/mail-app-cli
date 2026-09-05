import datetime
import hashlib
import json
import os
import sqlite3
import sys
import urllib.parse

native = json.load(sys.stdin)
if native.get("verified") is not True:
    raise SystemExit("Primary UI was not verified")
rows = native["rows"]
db = sqlite3.connect(
    "file:"
    + urllib.parse.quote(
        os.path.expanduser("~/Library/Mail/V10/MailData/Envelope Index")
    )
    + "?mode=ro",
    uri=True,
)


def same_date(stamp, label):
    # Mail's displayed dates use the Mac's local calendar and time zone.
    d = datetime.datetime.fromtimestamp(stamp)  # noqa: DTZ006
    today = datetime.datetime.now().date()  # noqa: DTZ005
    if label == "Yesterday":
        return d.date() == today - datetime.timedelta(days=1)
    if label == "Today":
        return d.date() == today
    for fmt in ("%d.%m.%y", "%d.%m.%Y", "%m/%d/%y", "%m/%d/%Y", "%Y-%m-%d"):
        try:
            return datetime.datetime.strptime(label, fmt).date() == d.date()  # noqa: DTZ007
        except ValueError:
            pass
    for fmt in ("%H:%M", "%I:%M %p"):
        if d.date() == today and d.strftime(fmt).lstrip("0") == label.lstrip("0"):
            return True
    return False


result = []
ambiguous = 0
missing = 0
for x in rows:
    matches = db.execute(
        "select m.rowid,m.read,m.date_received,m.remote_id,a.address,a.comment,b.url,g.message_id from messages m join subjects s on s.rowid=m.subject join addresses a on a.rowid=m.sender join mailboxes b on b.rowid=m.mailbox left join message_global_data g on g.rowid=m.global_message_id where s.subject=? and m.deleted=0 and m.read=? and (a.address=? or a.comment=?)",
        (x["subject"], int(not x["unread"]), x["sender"], x["sender"]),
    ).fetchall()
    exact = [
        r for r in matches if r[6].endswith("/INBOX") and same_date(r[2], x["date"])
    ]
    if len(exact) != 1:
        ambiguous += len(exact) > 1
        missing += not exact
        continue
    r = exact[0]
    if not r[3]:
        missing += 1
        continue
    result.append(
        {
            "id": hashlib.sha256((r[6] + "\0" + str(r[3])).encode()).hexdigest(),
            "localId": str(r[0]),
            "mailboxUrl": r[6],
            "category": "Primary",
            "sender": x["sender"],
            "subject": x["subject"],
            "received": r[2],
            "unread": x["unread"],
            "remoteId": str(r[3]),
            "rfcMessageId": r[7],
            "timeSensitive": x["timeSensitive"],
        }
    )
print(
    json.dumps(
        {
            "verified": True,
            "messages": result,
            "coverage": f"Recent Primary mail · {len(rows)} rows checked, {ambiguous + missing} unavailable",
            "scanned": len(rows),
            "matched": len(result),
            "ambiguous": ambiguous,
            "unmatched": missing,
        }
    )
)
