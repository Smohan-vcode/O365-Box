// Package keystore keeps small secrets on this machine (SPEC §21): the
// pipeline certificate's private key and provider tokens. It uses the OS
// keychain; where there is none it falls back to files encrypted with a key
// derived from BOX_KEYSTORE_PASSPHRASE (scrypt, AES-256-GCM).
package keystore

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zalando/go-keyring"

	"github.com/Smohan-vcode/O365-Box/engine/internal/draft"
)

const service = "box"

// ErrNotFound means no secret has that name.
var ErrNotFound = errors.New("secret not found")

// Store keeps named secrets.
type Store interface {
	Put(name string, value []byte) error
	Get(name string) ([]byte, error)
	Delete(name string) error
	Kind() string
}

// Open returns the keychain store, or the encrypted-file store in dir when
// the machine has no keychain (BOX_KEYSTORE_PASSPHRASE must then be set).
func Open(dir string) (Store, error) {
	if os.Getenv("BOX_KEYSTORE") != "file" && draft.KeychainAvailable() {
		return keychain{}, nil
	}
	pass := os.Getenv("BOX_KEYSTORE_PASSPHRASE")
	if len(pass) < 8 {
		return nil, errors.New("this machine has no OS keychain: set BOX_KEYSTORE_PASSPHRASE (8+ characters) to use the encrypted key files")
	}
	return &files{dir: dir, pass: pass}, nil
}

// DefaultDir is the encrypted-file location under the user config dir.
func DefaultDir() (string, error) {
	d, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "box", "keys"), nil
}

type keychain struct{}

func (keychain) Kind() string { return "keychain" }
func (keychain) Put(name string, v []byte) error {
	return keyring.Set(service, name, base64.StdEncoding.EncodeToString(v))
}
func (keychain) Get(name string) ([]byte, error) {
	s, err := keyring.Get(service, name)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(s)
}
func (keychain) Delete(name string) error {
	err := keyring.Delete(service, name)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

type files struct {
	dir, pass string
}

func (f *files) Kind() string { return "encrypted-file" }

func (f *files) store(name string) *draft.Store {
	safe := strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(name)
	return &draft.Store{Path: filepath.Join(f.dir, safe+".box-key"), Keys: draft.Passphrase{Secret: f.pass}}
}

func (f *files) Put(name string, v []byte) error {
	d := draft.New()
	d.Set("value", base64.StdEncoding.EncodeToString(v))
	return f.store(name).Save(d)
}

func (f *files) Get(name string) ([]byte, error) {
	s := f.store(name)
	if !s.Exists() {
		return nil, ErrNotFound
	}
	d, err := s.Load()
	if err != nil {
		return nil, fmt.Errorf("key file %s: %w", name, err)
	}
	return base64.StdEncoding.DecodeString(d.String("value"))
}

func (f *files) Delete(name string) error {
	return f.store(name).Delete()
}
