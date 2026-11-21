package draft

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRoundTripAndEncryption(t *testing.T) {
	p := filepath.Join(t.TempDir(), "d.box-draft")
	key := bytes.Repeat([]byte{7}, 32)
	s := &Store{Path: p, Keys: Static(key)}
	d := New()
	d.Step = "addresses"
	d.Set("company.legal_name", "Plaintext Detector Ltd")
	if err := s.Save(d); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(p)
	if bytes.Contains(raw, []byte("Plaintext Detector")) || bytes.Contains(raw, []byte("addresses")) {
		t.Fatal("draft is not encrypted")
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, want 0600", fi.Mode().Perm())
	}
	got, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Step != "addresses" || got.String("company.legal_name") != "Plaintext Detector Ltd" {
		t.Errorf("got %+v", got)
	}
	bad := &Store{Path: p, Keys: Static(bytes.Repeat([]byte{8}, 32))}
	if _, err := bad.Load(); !errors.Is(err, ErrWrongKey) {
		t.Errorf("wrong key: %v", err)
	}
	if err := s.Delete(); err != nil || s.Exists() {
		t.Errorf("delete: %v", err)
	}
}

func TestPassphrase(t *testing.T) {
	p := filepath.Join(t.TempDir(), "d.box-draft")
	s := &Store{Path: p, Keys: Passphrase{Secret: "correct horse battery"}}
	d := New()
	d.Step = "people"
	if err := s.Save(d); err != nil {
		t.Fatal(err)
	}
	if kdf, _ := s.KDF(); kdf != "scrypt" {
		t.Errorf("kdf %q", kdf)
	}
	if got, err := s.Load(); err != nil || got.Step != "people" {
		t.Fatalf("%v %v", got, err)
	}
	if err := s.Save(d); err != nil { // salt is kept across saves
		t.Fatal(err)
	}
	if _, err := (&Store{Path: p, Keys: Passphrase{Secret: "wrong passphrase!"}}).Load(); !errors.Is(err, ErrWrongKey) {
		t.Errorf("wrong passphrase: %v", err)
	}
	if _, err := (&Store{Path: p, Keys: Passphrase{Secret: "short"}}).Load(); err == nil {
		t.Error("short passphrase accepted")
	}
	if _, err := (&Store{Path: p, Keys: Static(bytes.Repeat([]byte{1}, 32))}).Load(); err == nil {
		t.Error("kind mismatch not reported")
	}
}
