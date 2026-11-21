// Package draft keeps the onboarding answers on this machine until the
// build stores them in the tenant (SPEC §9.1, §21). The file is encrypted
// with AES-256-GCM. The key lives in the OS keychain, or is derived from a
// passphrase (scrypt) when the machine has no keychain.
package draft

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/zalando/go-keyring"
	"golang.org/x/crypto/scrypt"
)

// Draft is the wizard's saved state.
type Draft struct {
	Version   int            `json:"version"`
	Step      string         `json:"step"` // screen to resume at
	Answers   map[string]any `json:"answers"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// New returns an empty draft.
func New() *Draft { return &Draft{Version: 1, Answers: map[string]any{}} }

// String returns an answer as a string.
func (d *Draft) String(key string) string {
	s, _ := d.Answers[key].(string)
	return s
}

// Set stores an answer.
func (d *Draft) Set(key string, v any) {
	if d.Answers == nil {
		d.Answers = map[string]any{}
	}
	d.Answers[key] = v
}

// KeySource supplies the 32-byte encryption key.
type KeySource interface {
	// Kind names the source in the file envelope: keychain or scrypt.
	Kind() string
	// Key returns the key. salt is the envelope's salt (scrypt only);
	// create allows making a new key when none exists.
	Key(salt []byte, create bool) ([]byte, error)
}

// ErrNoKey means the key for an existing draft is not available.
var ErrNoKey = errors.New("the key for this draft is not available")

// ErrWrongKey means the key did not decrypt the draft (wrong passphrase).
var ErrWrongKey = errors.New("the draft could not be decrypted with this key")

// Keychain keeps a random key in the OS keychain.
type Keychain struct{ Service, Account string }

func (k Keychain) Kind() string { return "keychain" }

func (k Keychain) Key(_ []byte, create bool) ([]byte, error) {
	v, err := keyring.Get(k.Service, k.Account)
	if err == nil {
		b, derr := base64.StdEncoding.DecodeString(v)
		if derr != nil || len(b) != 32 {
			return nil, fmt.Errorf("keychain entry %s/%s is not a draft key", k.Service, k.Account)
		}
		return b, nil
	}
	if !errors.Is(err, keyring.ErrNotFound) {
		return nil, fmt.Errorf("keychain unavailable: %w", err)
	}
	if !create {
		return nil, ErrNoKey
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	if err := keyring.Set(k.Service, k.Account, base64.StdEncoding.EncodeToString(b)); err != nil {
		return nil, fmt.Errorf("keychain unavailable: %w", err)
	}
	return b, nil
}

// Delete removes the key from the keychain.
func (k Keychain) Delete() error {
	err := keyring.Delete(k.Service, k.Account)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

// KeychainAvailable reports whether the OS keychain can store a key.
func KeychainAvailable() bool {
	const svc, acct = "box-probe", "probe"
	if err := keyring.Set(svc, acct, "x"); err != nil {
		return false
	}
	_ = keyring.Delete(svc, acct)
	return true
}

// Passphrase derives the key from a passphrase with scrypt.
type Passphrase struct{ Secret string }

func (p Passphrase) Kind() string { return "scrypt" }

func (p Passphrase) Key(salt []byte, _ bool) ([]byte, error) {
	if len(p.Secret) < 8 {
		return nil, errors.New("the passphrase must have at least 8 characters")
	}
	return scrypt.Key([]byte(p.Secret), salt, 1<<15, 8, 1, 32)
}

// Static is a fixed key, for tests.
type Static []byte

func (s Static) Kind() string                         { return "static" }
func (s Static) Key(_ []byte, _ bool) ([]byte, error) { return s, nil }

type envelope struct {
	V     int    `json:"v"`
	KDF   string `json:"kdf"`
	Salt  string `json:"salt,omitempty"`
	Nonce string `json:"nonce"`
	Data  string `json:"data"`
}

// Store reads and writes one encrypted draft file.
type Store struct {
	Path string
	Keys KeySource
}

// DefaultPath is the draft file in the user's config directory.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "box", "drafts", "onboarding.box-draft"), nil
}

// Exists reports whether a draft file is present.
func (s *Store) Exists() bool {
	_, err := os.Stat(s.Path)
	return err == nil
}

// KDF returns the key kind recorded in an existing draft file.
func (s *Store) KDF() (string, error) {
	env, err := s.read()
	if err != nil {
		return "", err
	}
	return env.KDF, nil
}

func (s *Store) read() (*envelope, error) {
	b, err := os.ReadFile(s.Path)
	if err != nil {
		return nil, err
	}
	var env envelope
	if err := json.Unmarshal(b, &env); err != nil || env.V != 1 {
		return nil, fmt.Errorf("%s is not a box draft", s.Path)
	}
	return &env, nil
}

// Save encrypts and writes the draft atomically, readable only by the user.
func (s *Store) Save(d *Draft) error {
	d.UpdatedAt = time.Now().UTC()
	plain, err := json.Marshal(d)
	if err != nil {
		return err
	}
	salt := make([]byte, 16)
	if env, err := s.read(); err == nil && env.Salt != "" {
		if b, err := base64.StdEncoding.DecodeString(env.Salt); err == nil {
			salt = b
		}
	} else if _, err := rand.Read(salt); err != nil {
		return err
	}
	key, err := s.Keys.Key(salt, true)
	if err != nil {
		return err
	}
	gcm, err := newGCM(key)
	if err != nil {
		return err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	env := envelope{V: 1, KDF: s.Keys.Kind(), Nonce: base64.StdEncoding.EncodeToString(nonce),
		Data: base64.StdEncoding.EncodeToString(gcm.Seal(nil, nonce, plain, []byte("box-draft-v1")))}
	if s.Keys.Kind() == "scrypt" {
		env.Salt = base64.StdEncoding.EncodeToString(salt)
	}
	b, err := json.Marshal(env)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	tmp := s.Path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.Path)
}

// Load reads and decrypts the draft.
func (s *Store) Load() (*Draft, error) {
	env, err := s.read()
	if err != nil {
		return nil, err
	}
	if env.KDF != s.Keys.Kind() {
		return nil, fmt.Errorf("this draft was saved with a %s key", env.KDF)
	}
	salt, _ := base64.StdEncoding.DecodeString(env.Salt)
	key, err := s.Keys.Key(salt, false)
	if err != nil {
		return nil, err
	}
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce, err1 := base64.StdEncoding.DecodeString(env.Nonce)
	data, err2 := base64.StdEncoding.DecodeString(env.Data)
	if err1 != nil || err2 != nil {
		return nil, fmt.Errorf("%s is damaged", s.Path)
	}
	plain, err := gcm.Open(nil, nonce, data, []byte("box-draft-v1"))
	if err != nil {
		return nil, ErrWrongKey
	}
	d := New()
	if err := json.Unmarshal(plain, d); err != nil {
		return nil, err
	}
	if d.Answers == nil {
		d.Answers = map[string]any{}
	}
	return d, nil
}

// Delete removes the draft file (and the keychain key when used).
func (s *Store) Delete() error {
	err := os.Remove(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		err = nil
	}
	if k, ok := s.Keys.(Keychain); ok {
		if kerr := k.Delete(); kerr != nil && err == nil {
			err = kerr
		}
	}
	return err
}

func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, errors.New("draft key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
