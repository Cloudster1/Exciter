// Package objectstore keeps the bytes of uploaded files in any bucket that
// speaks the S3 protocol. Adapted from Armature's attachment store.
package objectstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/google/uuid"
)

// Store is where the bytes go. A domain knows nothing about S3 beyond this, so
// a test can hold the bytes in memory and a deployment can point at any bucket.
type Store interface {
	Put(ctx context.Context, key string, body io.Reader, size int64, contentType string) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	// GetRange reads length bytes of an object from offset on, which the
	// caller has checked lie inside it.
	GetRange(ctx context.Context, key string, offset, length int64) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	// List names every object whose key starts with prefix.
	List(ctx context.Context, prefix string) ([]string, error)
}

// OrgPrefix is where every object of one organization lives, so all of an
// organization's files are found, and removed, by one listing.
func OrgPrefix(org uuid.UUID) string {
	return "org/" + org.String() + "/"
}

// DeletePrefix removes every object under prefix. An empty prefix is refused:
// it names the whole bucket.
func DeletePrefix(ctx context.Context, s Store, prefix string) error {
	if prefix == "" {
		return errors.New("refusing to delete every object in the bucket")
	}
	keys, err := s.List(ctx, prefix)
	if err != nil {
		return err
	}
	for _, key := range keys {
		if err := s.Delete(ctx, key); err != nil {
			return err
		}
	}
	return nil
}

// ErrUnavailable is returned when no object store is configured. It is a
// deployment problem, not a user's, and the message says which setting fixes it.
var ErrUnavailable = errors.New("file storage is not configured: set EXCITER_S3_ENDPOINT to an S3 compatible store")

// ErrNoObject is returned when a row names an object whose bytes are gone.
var ErrNoObject = errors.New("the file is missing from storage")

// Config is what it takes to reach a bucket.
type Config struct {
	Endpoint  string
	Bucket    string
	AccessKey string
	SecretKey string
	Region    string
	UseSSL    bool
}

// Configured reports whether a store was asked for at all.
func (c Config) Configured() bool { return c.Endpoint != "" }

// Open returns the bucket a configuration names, or a store that refuses with
// the setting to fix. It does not pretend.
func Open(cfg Config) (Store, error) {
	if !cfg.Configured() {
		return Unavailable{}, nil
	}
	return NewS3(cfg)
}

// IsUnavailable reports whether a store is the one that refuses everything.
func IsUnavailable(s Store) bool {
	if s == nil {
		return true
	}
	_, off := s.(Unavailable)
	return off
}

// Memory holds objects in a map. It is for tests, and for nothing else.
type Memory struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func NewMemory() *Memory { return &Memory{objects: map[string][]byte{}} }

func (m *Memory) Put(_ context.Context, key string, body io.Reader, _ int64, _ string) error {
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = data
	return nil
}

func (m *Memory) Get(_ context.Context, key string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.objects[key]
	if !ok {
		return nil, ErrNoObject
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (m *Memory) GetRange(_ context.Context, key string, offset, length int64) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.objects[key]
	if !ok {
		return nil, ErrNoObject
	}
	if offset < 0 || length < 0 || offset+length > int64(len(data)) {
		return nil, fmt.Errorf("fetch object: bytes %d to %d lie outside its %d", offset, offset+length, len(data))
	}
	return io.NopCloser(bytes.NewReader(data[offset : offset+length])), nil
}

func (m *Memory) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects, key)
	return nil
}

func (m *Memory) List(_ context.Context, prefix string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var keys []string
	for key := range m.objects {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

// Len is how many objects are held, for a test to count.
func (m *Memory) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.objects)
}

// Unavailable is the store a deployment gets when none was configured. Every
// call fails the same way, so an upload is refused with the setting to fix.
type Unavailable struct{}

func (Unavailable) Put(context.Context, string, io.Reader, int64, string) error {
	return ErrUnavailable
}
func (Unavailable) Get(context.Context, string) (io.ReadCloser, error) { return nil, ErrUnavailable }
func (Unavailable) GetRange(context.Context, string, int64, int64) (io.ReadCloser, error) {
	return nil, ErrUnavailable
}
func (Unavailable) Delete(context.Context, string) error           { return ErrUnavailable }
func (Unavailable) List(context.Context, string) ([]string, error) { return nil, ErrUnavailable }

// MaxNameLength keeps a file name short enough for a header and a listing.
const MaxNameLength = 200

// CleanName reduces a file name to something safe to store and to send back
// in a header: no directories, no control characters, never empty.
func CleanName(name string) string {
	name = path.Base(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"))
	var b strings.Builder
	for _, r := range name {
		if unicode.IsControl(r) || r == '"' || r == '/' {
			continue
		}
		b.WriteRune(r)
	}
	name = b.String()
	if name == "" || name == "." || name == ".." {
		return "file"
	}
	if runes := []rune(name); len(runes) > MaxNameLength {
		name = string(runes[:MaxNameLength])
	}
	return name
}
