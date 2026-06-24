package desktop

import "sync"

type SecretStore interface {
	Put(key string, value string) error
	Get(key string) (string, error)
	Delete(key string) error
}

type MemorySecretStore struct {
	mu      sync.Mutex
	secrets map[string]string
}

func NewMemorySecretStore() *MemorySecretStore {
	return &MemorySecretStore{secrets: map[string]string{}}
}

func (s *MemorySecretStore) Put(key string, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.secrets[key] = value
	return nil
}

func (s *MemorySecretStore) Get(key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.secrets[key], nil
}

func (s *MemorySecretStore) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.secrets, key)
	return nil
}
