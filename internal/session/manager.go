package session

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"time"
)

type Manager struct {
	store   *Store
	current *Session
}

func NewManager(sessionsDir string) *Manager {
	return &Manager{
		store: NewStore(sessionsDir),
	}
}

func (m *Manager) NewSession() *Session {
	id := generateID()
	m.current = &Session{
		ID:        id,
		CreatedAt: time.Now(),
		Title:     "New session",
		CWD:       getCWD(),
	}
	return m.current
}

func (m *Manager) Current() *Session {
	return m.current
}

func (m *Manager) Resume(id string) (*Session, error) {
	sess, err := m.store.Load(id)
	if err != nil {
		return nil, err
	}
	m.current = sess
	return sess, nil
}

func (m *Manager) Save() error {
	if m.current == nil {
		return nil
	}
	return m.store.Save(m.current)
}

func (m *Manager) List() ([]SessionSummary, error) {
	return m.store.List()
}

func (m *Manager) Delete(id string) error {
	return m.store.Delete(id)
}

func generateID() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-only ID if entropy fails
		return time.Now().Format("2006-01-02-150405")
	}
	return fmt.Sprintf("%s-%s", time.Now().Format("2006-01-02"), hex.EncodeToString(b))
}

func getCWD() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return cwd
}
