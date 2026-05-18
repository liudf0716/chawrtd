package ws

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

var autoAliasPattern = regexp.MustCompile(`(?i)^wifi(\d+)$`)

// AliasStore manages device alias persistence
type AliasStore struct {
	mu       sync.RWMutex
	aliases  map[string]string
	filePath string
}

// NewAliasStore creates a new alias store
func NewAliasStore(filePath string) (*AliasStore, error) {
	store := &AliasStore{
		aliases:  make(map[string]string),
		filePath: filePath,
	}

	// Create directory if it doesn't exist
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	// Load existing aliases
	if err := store.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load aliases: %w", err)
	}

	return store, nil
}

// Get returns the alias for a device
func (s *AliasStore) Get(deviceID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.aliases[deviceID]
}

// Set sets the alias for a device and persists it
func (s *AliasStore) Set(deviceID, alias string) error {
	s.mu.Lock()
	s.aliases[deviceID] = alias
	s.mu.Unlock()
	return s.persist()
}

// Delete removes the alias for a device
func (s *AliasStore) Delete(deviceID string) error {
	s.mu.Lock()
	delete(s.aliases, deviceID)
	s.mu.Unlock()
	return s.persist()
}

// List returns all aliases
func (s *AliasStore) List() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]string)
	for k, v := range s.aliases {
		result[k] = v
	}
	return result
}

// EnsureAutoAlias returns existing alias or creates/persists a new WiFiN alias.
func (s *AliasStore) EnsureAutoAlias(deviceID string) (string, bool, error) {
	s.mu.Lock()
	if existing := s.aliases[deviceID]; existing != "" {
		s.mu.Unlock()
		return existing, false, nil
	}

	next := s.nextAutoAliasNumberLocked()
	alias := fmt.Sprintf("WiFi%d", next)
	s.aliases[deviceID] = alias
	s.mu.Unlock()

	if err := s.persist(); err != nil {
		return "", false, err
	}
	return alias, true, nil
}

func (s *AliasStore) nextAutoAliasNumberLocked() int {
	maxIdx := 0
	for _, alias := range s.aliases {
		trimmed := strings.TrimSpace(alias)
		m := autoAliasPattern.FindStringSubmatch(trimmed)
		if len(m) != 2 {
			continue
		}
		idx, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		if idx > maxIdx {
			maxIdx = idx
		}
	}
	return maxIdx + 1
}

// load reads aliases from the file
func (s *AliasStore) load() error {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return err
	}

	var aliases map[string]string
	if err := json.Unmarshal(data, &aliases); err != nil {
		return fmt.Errorf("invalid JSON in alias file: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.aliases = aliases
	return nil
}

// persist writes aliases to the file
func (s *AliasStore) persist() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := json.MarshalIndent(s.aliases, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal aliases: %w", err)
	}

	return os.WriteFile(s.filePath, data, 0644)
}
