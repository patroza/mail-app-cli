package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDraftLostAcknowledgementRetry(t *testing.T) {
	dir := t.TempDir()
	q := Request{Op: "draft-put", DraftID: strings.Repeat("b", 32), Draft: json.RawMessage(`{ "subject": "first" }`)}
	first, err := draftInDir(dir, q)
	if err != nil {
		t.Fatal(err)
	}
	// The first response was lost: retry has no revision, exactly as a client
	// recovering from a broken SSH connection would send it.
	retry, err := draftInDir(dir, q)
	if err != nil || retry["revision"] != first["revision"] {
		t.Fatalf("initial retry: %v %v", retry, err)
	}
	q.Revision = first["revision"].(string)
	q.Draft = json.RawMessage(`{"subject":"second","attachments":[{"data":"YWJj"}],"body":"unsent"}`)
	second, err := draftInDir(dir, q)
	if err != nil {
		t.Fatal(err)
	}
	retry, err = draftInDir(dir, q)
	if err != nil || retry["revision"] != second["revision"] {
		t.Fatalf("update retry: %v %v", retry, err)
	}
	q.Draft = json.RawMessage(`{"subject":"stale different content"}`)
	if _, err = draftInDir(dir, q); err == nil {
		t.Fatal("stale different content must conflict")
	}
}

func TestDraftListReportsCorruptionAndPreservesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, strings.Repeat("c", 32)+".json")
	for _, raw := range []string{"broken", "{}"} {
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := draftInDir(dir, Request{Op: "draft-list"}); err == nil {
			t.Fatal("corrupt draft silently disappeared")
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != raw {
			t.Fatal("corrupt draft was not preserved")
		}
	}
}

func TestDraftDirectorySyncFailureIsReported(t *testing.T) {
	if err := syncDraftDirectory(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing directory acknowledged as durable")
	}
}

func TestDraftDeleteMissingIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	q := Request{Op: "draft-put", DraftID: strings.Repeat("d", 32), Draft: json.RawMessage(`{"subject":"explicit discard"}`)}
	saved, err := draftInDir(dir, q)
	if err != nil {
		t.Fatal(err)
	}
	q.Op = "draft-delete"
	q.Revision = saved["revision"].(string)
	if _, err = draftInDir(dir, q); err != nil {
		t.Fatal(err)
	}
	// Retry a lost delete acknowledgement, or discard local recovery after the
	// other host has already removed the corresponding Mac draft.
	if _, err = draftInDir(dir, q); err != nil {
		t.Fatalf("missing delete: %v", err)
	}
	q.Op = "draft-put"
	if _, err = draftInDir(dir, q); err == nil {
		t.Fatal("stale put must not resurrect a deleted draft")
	}
}
