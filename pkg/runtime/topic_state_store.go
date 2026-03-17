package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// TopicState records one chat-topic to Codex-thread binding persisted on disk.
type TopicState struct {
	ChatID         string    `json:"chat_id"`
	FeishuThreadID string    `json:"feishu_thread_id"`
	ProjectAlias   string    `json:"project_alias"`
	CodexThreadID  string    `json:"codex_thread_id"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// TopicStateStore manages in-memory topic bindings with atomic JSON persistence.
type TopicStateStore struct {
	path    string
	mu      sync.RWMutex
	entries map[string]TopicState
}

type topicStateFile struct {
	Bindings []TopicState `json:"bindings"`
}

// NewTopicStateStore creates a TopicStateStore and loads existing bindings from path.
func NewTopicStateStore(path string) (*TopicStateStore, error) {
	store := &TopicStateStore{
		path:    path,
		entries: map[string]TopicState{},
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

// Get returns a stored topic binding by chat and Feishu thread IDs.
func (s *TopicStateStore) Get(chatID, feishuThreadID string) (TopicState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.entries[stateKey(chatID, feishuThreadID)]
	return entry, ok
}

// Upsert validates and stores a topic binding, then persists the full state file.
func (s *TopicStateStore) Upsert(state TopicState) error {
	state.ChatID = strings.TrimSpace(state.ChatID)
	state.FeishuThreadID = strings.TrimSpace(state.FeishuThreadID)
	state.ProjectAlias = strings.TrimSpace(state.ProjectAlias)
	state.CodexThreadID = strings.TrimSpace(state.CodexThreadID)

	switch {
	case state.ChatID == "":
		return fmt.Errorf("chat_id is required")
	case state.FeishuThreadID == "":
		return fmt.Errorf("feishu_thread_id is required")
	case state.ProjectAlias == "":
		return fmt.Errorf("project_alias is required")
	case state.CodexThreadID == "":
		return fmt.Errorf("codex_thread_id is required")
	}

	if state.UpdatedAt.IsZero() {
		state.UpdatedAt = time.Now().UTC()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[stateKey(state.ChatID, state.FeishuThreadID)] = state
	return s.persistLocked()
}

func (s *TopicStateStore) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read topic state %q: %w", s.path, err)
	}
	var payload topicStateFile
	if err := json.Unmarshal(data, &payload); err != nil {
		return fmt.Errorf("decode topic state %q: %w", s.path, err)
	}
	for _, binding := range payload.Bindings {
		if strings.TrimSpace(binding.ChatID) == "" || strings.TrimSpace(binding.FeishuThreadID) == "" {
			continue
		}
		s.entries[stateKey(binding.ChatID, binding.FeishuThreadID)] = binding
	}
	return nil
}

func (s *TopicStateStore) persistLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("mkdir topic state dir: %w", err)
	}

	payload := topicStateFile{Bindings: make([]TopicState, 0, len(s.entries))}
	for _, binding := range s.entries {
		payload.Bindings = append(payload.Bindings, binding)
	}
	sort.Slice(payload.Bindings, func(i, j int) bool {
		left := stateKey(payload.Bindings[i].ChatID, payload.Bindings[i].FeishuThreadID)
		right := stateKey(payload.Bindings[j].ChatID, payload.Bindings[j].FeishuThreadID)
		return left < right
	})

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal topic state: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write topic state temp file: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("rename topic state file: %w", err)
	}
	return nil
}

func stateKey(chatID, threadID string) string {
	return strings.TrimSpace(chatID) + "::" + strings.TrimSpace(threadID)
}
