package runtime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTopicStateStore_PersistsAndReloads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state", "topic.json")

	store, err := NewTopicStateStore(path)
	if err != nil {
		t.Fatalf("NewTopicStateStore error: %v", err)
	}

	err = store.Upsert(TopicState{
		ChatID:        "oc_chat",
		FeishuThreadID: "omt_thread",
		ProjectAlias:  "tidb",
		CodexThreadID: "codex_123",
	})
	if err != nil {
		t.Fatalf("Upsert error: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("state file not written: %v", err)
	}

	reloaded, err := NewTopicStateStore(path)
	if err != nil {
		t.Fatalf("reload error: %v", err)
	}

	got, ok := reloaded.Get("oc_chat", "omt_thread")
	if !ok {
		t.Fatal("expected binding after reload")
	}
	if got.ProjectAlias != "tidb" || got.CodexThreadID != "codex_123" {
		t.Fatalf("unexpected binding: %+v", got)
	}
	if got.UpdatedAt.IsZero() {
		t.Fatal("expected updated_at to be filled")
	}
}

func TestTopicStateStore_UpsertOverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "topic.json")
	store, err := NewTopicStateStore(path)
	if err != nil {
		t.Fatalf("NewTopicStateStore error: %v", err)
	}

	if err := store.Upsert(TopicState{ChatID: "oc_chat", FeishuThreadID: "omt", ProjectAlias: "a", CodexThreadID: "t1"}); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if err := store.Upsert(TopicState{ChatID: "oc_chat", FeishuThreadID: "omt", ProjectAlias: "b", CodexThreadID: "t2"}); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	got, ok := store.Get("oc_chat", "omt")
	if !ok {
		t.Fatal("expected entry")
	}
	if got.ProjectAlias != "b" || got.CodexThreadID != "t2" {
		t.Fatalf("entry not updated: %+v", got)
	}
}
