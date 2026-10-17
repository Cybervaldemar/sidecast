package channels

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sync"
)

var (
	ErrTaken        = errors.New("nickname_taken")
	ErrInvalid      = errors.New("invalid_nickname")
	ErrFull         = errors.New("channel_limit")
	nicknamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{2,31}$`)
)

func Valid(nickname string) bool { return nicknamePattern.MatchString(nickname) }

type Store struct {
	mu     sync.RWMutex
	path   string
	owners map[string]string
}

func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(dir, "channels.json"), owners: map[string]string{}}
	b, err := os.ReadFile(s.path)
	if err == nil {
		err = json.Unmarshal(b, &s.owners)
	}
	if os.IsNotExist(err) {
		err = nil
	}
	if s.owners == nil {
		return nil, errors.New("invalid channel database")
	}
	return s, err
}

func Token() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func digest(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func (s *Store) Owns(nickname, token string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	h, ok := s.owners[nickname]
	return ok && token != "" && subtle.ConstantTimeCompare([]byte(h), []byte(digest(token))) == 1
}

func (s *Store) Exists(nickname string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.owners[nickname]
	return ok
}

// A nickname is reserved permanently; only its secret can reuse it.
// Persist before returning the secret, so a failed write cannot lose ownership.
func (s *Store) Claim(nickname, token string) (string, error) {
	if !Valid(nickname) {
		return "", ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if h, ok := s.owners[nickname]; ok {
		if token != "" && subtle.ConstantTimeCompare([]byte(h), []byte(digest(token))) == 1 {
			return token, nil
		}
		return "", ErrTaken
	}
	if len(s.owners) >= 1024 {
		return "", ErrFull
	}
	token = Token()
	s.owners[nickname] = digest(token)
	b, err := json.MarshalIndent(s.owners, "", "  ")
	if err == nil {
		err = os.WriteFile(s.path+".tmp", b, 0600)
	}
	if err == nil {
		err = os.Rename(s.path+".tmp", s.path)
	}
	if err != nil {
		delete(s.owners, nickname)
		return "", err
	}
	return token, nil
}
