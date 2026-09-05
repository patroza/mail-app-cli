"""Fixture integration tests for the macOS Primary database resolver."""

import datetime
import hashlib
import json
import os
import sqlite3
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).with_name("primary.py")
MAILBOX = "imap://fixture-account/INBOX"
# Match the local displayed calendar date regardless of the test host's zone.
STAMP = int(datetime.datetime(2026, 9, 5, 12).timestamp())  # noqa: DTZ001


class PrimaryMatcherTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.home = Path(self.temp.name)
        self.path = self.home / "Library/Mail/V10/MailData/Envelope Index"
        self.path.parent.mkdir(parents=True)
        self.db = sqlite3.connect(self.path)
        self.addCleanup(self.db.close)
        self.db.executescript("""
            CREATE TABLE messages (
                read INTEGER, date_received INTEGER, remote_id TEXT,
                subject INTEGER, sender INTEGER, mailbox INTEGER,
                global_message_id INTEGER, deleted INTEGER);
            CREATE TABLE subjects (subject TEXT);
            CREATE TABLE addresses (address TEXT, comment TEXT);
            CREATE TABLE mailboxes (url TEXT);
            CREATE TABLE message_global_data (message_id TEXT);
            INSERT INTO subjects VALUES ('Fixture subject');
            INSERT INTO addresses VALUES ('sender@example.invalid', 'Fixture Sender');
            INSERT INTO message_global_data VALUES ('<fixture@example.invalid>');
        """)
        self.db.execute("INSERT INTO mailboxes VALUES (?)", (MAILBOX,))
        self.db.execute(
            "INSERT INTO messages VALUES (0, ?, '42', 1, 1, 1, 1, 0)",
            (STAMP,),
        )
        self.db.commit()

    def row(self, **changes):
        return {
            "subject": "Fixture subject",
            "sender": "Fixture Sender",
            "date": "05.09.26",
            "unread": True,
            "timeSensitive": False,
        } | changes

    def run_match(self, rows=None, verified=True, readonly_probe=False):
        payload = {"verified": verified, "rows": rows or [self.row()], "limit": 100}
        command = [sys.executable, str(SCRIPT)]
        if readonly_probe:
            # Exercise the resolver's actual connection parameters, and prove
            # that SQLite itself refuses writes to this disposable fixture.
            command = [
                sys.executable,
                "-c",
                """
import runpy, sqlite3, sys
original = sqlite3.connect
def checked(database_uri, **kwargs):
    assert database_uri.endswith('?mode=ro') and kwargs.get('uri') is True
    connection = original(database_uri, **kwargs)
    try:
        connection.execute('UPDATE messages SET read=1')
    except sqlite3.OperationalError as error:
        assert 'readonly' in str(error)
    else:
        raise AssertionError('Mail database was writable')
    return connection
sqlite3.connect = checked
runpy.run_path(sys.argv[1], run_name='__main__')
""",
                str(SCRIPT),
            ]
        before = self.path.read_bytes()
        result = subprocess.run(
            command,
            check=False,
            input=json.dumps(payload),
            text=True,
            capture_output=True,
            env=os.environ | {"HOME": str(self.home)},
        )
        self.assertEqual(
            self.path.read_bytes(), before, "Resolver changed the database"
        )
        return result

    def result(self, **kwargs):
        process = self.run_match(**kwargs)
        self.assertEqual(process.returncode, 0, process.stderr)
        return json.loads(process.stdout)

    def test_unique_match_is_stable_across_local_row_id_changes(self):
        first = self.result()["messages"][0]
        expected = hashlib.sha256((MAILBOX + "\0" + "42").encode()).hexdigest()
        self.assertEqual(first["id"], expected)
        self.assertEqual(first["category"], "Primary")
        self.assertEqual(first["received"], STAMP)
        self.assertTrue(first["unread"])
        self.db.execute("UPDATE messages SET rowid=99")
        self.db.commit()
        second = self.result()["messages"][0]
        self.assertEqual(first["id"], second["id"])
        self.assertNotEqual(first["localId"], second["localId"])

    def test_ambiguous_matches_are_omitted(self):
        self.db.execute("INSERT INTO messages SELECT * FROM messages")
        self.db.commit()
        result = self.result()
        self.assertEqual(result["messages"], [])
        self.assertEqual(result["ambiguous"], 1)

    def test_unknown_date_wrong_date_and_read_mismatch_are_omitted(self):
        for changes in (
            {"date": "unrecognized"},
            {"date": "04.09.26"},
            {"unread": False},
        ):
            with self.subTest(changes=changes):
                result = self.result(rows=[self.row(**changes)])
                self.assertEqual(result["messages"], [])
                self.assertEqual(result["unmatched"], 1)

    def test_deleted_noninbox_and_missing_uid_are_omitted(self):
        for query in (
            "UPDATE messages SET deleted=1",
            "UPDATE mailboxes SET url='imap://fixture-account/Archive'",
            "UPDATE messages SET remote_id=NULL",
        ):
            with self.subTest(query=query):
                self.db.execute("SAVEPOINT fixture")
                self.db.execute(query)
                self.db.execute("RELEASE fixture")
                self.assertEqual(self.result()["messages"], [])
                self.db.execute("UPDATE messages SET deleted=0, remote_id='42'")
                self.db.execute("UPDATE mailboxes SET url=?", (MAILBOX,))
                self.db.commit()

    def test_database_is_opened_readonly_and_read_flags_unchanged(self):
        result = self.result(readonly_probe=True)
        self.assertEqual(result["matched"], 1)
        self.assertEqual(self.db.execute("SELECT read FROM messages").fetchone()[0], 0)

    def test_unverified_ui_is_rejected(self):
        process = self.run_match(verified=False)
        self.assertNotEqual(process.returncode, 0)
        self.assertIn("Primary UI was not verified", process.stderr)


if __name__ == "__main__":
    unittest.main()
