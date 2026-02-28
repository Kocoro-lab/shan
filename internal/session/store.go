package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Kocoro-lab/shan/internal/client"
)

type Session struct {
	ID          string           `json:"id"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
	Title       string           `json:"title"`
	CWD         string           `json:"cwd"`
	Messages    []client.Message `json:"messages"`
	RemoteTasks []string         `json:"remote_tasks,omitempty"`
}

type SessionSummary struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	MsgCount  int       `json:"msg_count"`
}

type Store struct {
	dir string
}

func NewStore(dir string) *Store {
	os.MkdirAll(dir, 0700)
	return &Store{dir: dir}
}

func (s *Store) Save(sess *Session) error {
	sess.UpdatedAt = time.Now()
	if sess.CreatedAt.IsZero() {
		sess.CreatedAt = sess.UpdatedAt
	}

	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}

	path := filepath.Join(s.dir, sess.ID+".json")
	return os.WriteFile(path, data, 0600)
}

func (s *Store) Load(id string) (*Session, error) {
	path := filepath.Join(s.dir, id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read session: %w", err)
	}

	var sess Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, fmt.Errorf("parse session: %w", err)
	}
	return &sess, nil
}

func (s *Store) List() ([]SessionSummary, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}

	var summaries []SessionSummary
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		sess, err := s.Load(id)
		if err != nil {
			continue
		}
		summaries = append(summaries, SessionSummary{
			ID:        sess.ID,
			Title:     sess.Title,
			CreatedAt: sess.CreatedAt,
			MsgCount:  len(sess.Messages),
		})
	}

	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].CreatedAt.After(summaries[j].CreatedAt)
	})
	return summaries, nil
}

func (s *Store) Delete(id string) error {
	path := filepath.Join(s.dir, id+".json")
	return os.Remove(path)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
