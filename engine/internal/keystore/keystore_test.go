package keystore

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestEncryptedFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BOX_KEYSTORE", "file")
	t.Setenv("BOX_KEYSTORE_PASSPHRASE", "")
	if _, err := Open(dir); err == nil {
		t.Fatal("want error without passphrase")
	}
	t.Setenv("BOX_KEYSTORE_PASSPHRASE", "a long test passphrase")
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("get missing: %v", err)
	}
	secret := []byte("-----PRIVATE MATERIAL 12345-----")
	if err := s.Put("tenant/app", secret); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("tenant/app")
	if err != nil || !bytes.Equal(got, secret) {
		t.Fatalf("got %q %v", got, err)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*"))
	for _, f := range files {
		b, _ := os.ReadFile(f)
		if bytes.Contains(b, []byte("PRIVATE MATERIAL")) {
			t.Error("secret stored in plaintext")
		}
	}
	t.Setenv("BOX_KEYSTORE_PASSPHRASE", "another wrong passphrase")
	s2, _ := Open(dir)
	if _, err := s2.Get("tenant/app"); err == nil {
		t.Error("wrong passphrase opened the key")
	}
	if err := s.Delete("tenant/app"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("tenant/app"); !errors.Is(err, ErrNotFound) {
		t.Error("not deleted")
	}
}
