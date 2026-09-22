package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// A Share is one file the daemon hands out, under the alias the user picked.
type Share struct {
	Alias string `json:"alias"`
	Path  string `json:"path"`
}

// Registry owns the share list. The control socket mutates it while the HTTP
// handler reads it, so every access takes the lock, and every change reaches
// disk before it is reported as done.
type Registry struct {
	mu     sync.RWMutex
	file   string
	shares map[string]Share
}

func LoadRegistry(file string) (*Registry, error) {
	r := &Registry{file: file, shares: map[string]Share{}}

	data, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read registry %s: %w", file, err)
	}

	var shares []Share
	if err := json.Unmarshal(data, &shares); err != nil {
		return nil, fmt.Errorf("parse registry %s: %w", file, err)
	}
	for _, share := range shares {
		r.shares[share.Alias] = share
	}
	return r, nil
}

func (r *Registry) Add(path, alias string) (Share, error) {
	file, err := resolveFile(path)
	if err != nil {
		return Share{}, err
	}
	if alias == "" {
		alias = filepath.Base(file)
	}
	if err := validateAlias(alias); err != nil {
		return Share{}, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if taken, exists := r.shares[alias]; exists && taken.Path != file {
		return Share{}, fmt.Errorf("alias %q already serves %s", alias, taken.Path)
	}

	share := Share{Alias: alias, Path: file}
	r.shares[alias] = share
	if err := r.save(); err != nil {
		delete(r.shares, alias)
		return Share{}, err
	}
	return share, nil
}

func (r *Registry) Remove(alias string) (Share, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	share, exists := r.shares[alias]
	if !exists {
		return Share{}, fmt.Errorf("no share named %q", alias)
	}

	delete(r.shares, alias)
	if err := r.save(); err != nil {
		r.shares[alias] = share
		return Share{}, err
	}
	return share, nil
}

func (r *Registry) List() []Share {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.sorted()
}

// sorted is the snapshot both List and save need. The caller holds the lock,
// which is why save can reach it and List cannot simply call itself.
func (r *Registry) sorted() []Share {
	shares := make([]Share, 0, len(r.shares))
	for _, share := range r.shares {
		shares = append(shares, share)
	}
	sort.Slice(shares, func(i, j int) bool { return shares[i].Alias < shares[j].Alias })
	return shares
}

// A DatedShare is a share with the time its file last changed. The registry
// records what is shared; the disk records when it changed, so the time is
// read when it is asked for and never stored.
type DatedShare struct {
	Share
	Modified time.Time
}

// ListByNewest orders the shares the way a reader wants them: the file that
// changed last comes first, because that is the one they were just sent.
// A file peek can no longer stat keeps the zero time and falls to the end.
func (r *Registry) ListByNewest() []DatedShare {
	shares := r.List()

	dated := make([]DatedShare, 0, len(shares))
	for _, share := range shares {
		var modified time.Time
		if info, err := os.Stat(share.Path); err == nil {
			modified = info.ModTime()
		}
		dated = append(dated, DatedShare{Share: share, Modified: modified})
	}

	sort.SliceStable(dated, func(i, j int) bool { return dated[i].Modified.After(dated[j].Modified) })
	return dated
}

func (r *Registry) Lookup(alias string) (Share, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	share, exists := r.shares[alias]
	return share, exists
}

// save writes the whole list through a temporary file, so a crash mid-write
// leaves the previous registry intact. The caller holds the lock.
func (r *Registry) save() error {
	data, err := json.MarshalIndent(r.sorted(), "", "  ")
	if err != nil {
		return fmt.Errorf("encode registry: %w", err)
	}

	dir := filepath.Dir(r.file)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}

	temp, err := os.CreateTemp(dir, "registry-*.json")
	if err != nil {
		return fmt.Errorf("create temporary registry: %w", err)
	}
	defer os.Remove(temp.Name())

	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return fmt.Errorf("write temporary registry: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temporary registry: %w", err)
	}
	if err := os.Rename(temp.Name(), r.file); err != nil {
		return fmt.Errorf("replace registry %s: %w", r.file, err)
	}
	return nil
}

func resolveFile(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", path, err)
	}

	// Symlinks are followed once, here, so the registry holds the real file and
	// the daemon never chases a link that changed after registration.
	file, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", path, err)
	}

	info, err := os.Stat(file)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", file, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory; peek shares single files", file)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", file)
	}
	return file, nil
}

// An alias becomes one path segment of the URL, so it must not carry a
// separator or climb the tree.
func validateAlias(alias string) error {
	switch {
	case alias == "":
		return errors.New("alias must not be empty")
	case strings.ContainsRune(alias, '/'):
		return fmt.Errorf("alias %q must not contain %q", alias, "/")
	case alias == "." || alias == "..":
		return fmt.Errorf("alias %q is reserved", alias)
	}
	return nil
}
