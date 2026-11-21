// Package repo locates the engine repository and walks its text files.
package repo

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// FindRoot walks up from dir to the directory holding go.mod and docs/SPEC.md.
func FindRoot(dir string) (string, error) {
	d, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		if exists(filepath.Join(d, "go.mod")) && exists(filepath.Join(d, "docs", "SPEC.md")) {
			return d, nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", errors.New("not inside the engine repository (no go.mod and docs/SPEC.md found); use --repo")
		}
		d = parent
	}
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// SkipDirs are never scanned.
var SkipDirs = map[string]bool{".git": true, "bin": true, "dist": true, "node_modules": true, ".terraform": true}

const maxTextSize = 2 << 20

// TextFiles returns paths (relative to root, slash-separated) of text files
// under root, skipping SkipDirs and any directory for which skip returns true.
func TextFiles(root string, skip func(rel string) bool) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel != "." && (SkipDirs[d.Name()] || (skip != nil && skip(rel))) {
				return filepath.SkipDir
			}
			return nil
		}
		if skip != nil && skip(rel) {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > maxTextSize {
			return nil
		}
		if IsText(p) {
			out = append(out, rel)
		}
		return nil
	})
	return out, err
}

// IsText reports whether the file's first 8 KB contain no NUL byte.
func IsText(p string) bool {
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 8192)
	n, _ := f.Read(buf)
	return !bytes.Contains(buf[:n], []byte{0})
}

// HasPrefixDir reports whether rel is dir or inside it.
func HasPrefixDir(rel, dir string) bool {
	dir = strings.TrimSuffix(dir, "/")
	return rel == dir || strings.HasPrefix(rel, dir+"/")
}
