package receipt

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Store manages the receipt directory at ~/.cloneable/receipts/.
type Store struct {
	dir string // absolute path to the receipts directory
}

// NewStore creates a Store for the given receipts directory.
// Creates the directory if it doesn't exist.
func NewStore(receiptsDir string) (*Store, error) {
	if err := os.MkdirAll(receiptsDir, 0755); err != nil {
		return nil, fmt.Errorf("could not create receipts directory: %w", err)
	}
	return &Store{dir: receiptsDir}, nil
}

// Save writes a receipt to disk, overwriting any existing receipt for that repo.
func (s *Store) Save(r *Receipt) error {
	r.UpdatedAt = time.Now()
	if r.InstalledAt.IsZero() {
		r.InstalledAt = r.UpdatedAt
	}

	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("could not encode receipt: %w", err)
	}

	path := s.receiptPath(r.Name)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("could not write receipt to %s: %w", path, err)
	}
	return nil
}

// Load reads the receipt for the given repo name.
// Returns (nil, nil) if no receipt exists — not an error.
func (s *Store) Load(name string) (*Receipt, error) {
	path := s.receiptPath(name)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("could not read receipt for %s: %w", name, err)
	}

	var r Receipt
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("could not parse receipt for %s: %w", name, err)
	}
	return &r, nil
}

// Delete removes the receipt file for the given repo name.
func (s *Store) Delete(name string) error {
	path := s.receiptPath(name)
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil // Already gone — not an error
	}
	return err
}

// All returns all receipts in the store, sorted alphabetically by name.
func (s *Store) All() ([]*Receipt, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("could not read receipts directory: %w", err)
	}

	var receipts []*Receipt
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		name := strings.TrimSuffix(entry.Name(), ".json")
		r, err := s.Load(name)
		if err != nil {
			// Skip corrupted receipt — don't crash the whole list
			continue
		}
		if r != nil {
			receipts = append(receipts, r)
		}
	}
	return receipts, nil
}

// Exists returns true if a receipt for the given name exists.
func (s *Store) Exists(name string) bool {
	_, err := os.Stat(s.receiptPath(name))
	return err == nil
}

// receiptPath returns the full path to a receipt file.
func (s *Store) receiptPath(name string) string {
	// Sanitise name to be a safe filename
	safe := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' {
			return '-'
		}
		return r
	}, name)
	return filepath.Join(s.dir, safe+".json")
}

// Remove fully removes an installation:
//  1. Removes all tracked symlinks
//  2. Removes the env directory (e.g. .venv, node_modules)
//  3. Optionally removes the cloned repo directory
//  4. Deletes the receipt
func (s *Store) Remove(name string, removeRepo bool) error {
	r, err := s.Load(name)
	if err != nil {
		return err
	}
	if r == nil {
		return fmt.Errorf("no installation found for %q — nothing to remove", name)
	}

	// 1. Remove symlinks
	for _, sym := range r.Symlinks {
		if err := os.Remove(sym.Target); err != nil && !os.IsNotExist(err) {
			fmt.Printf("  warning: could not remove symlink %s: %v\n", sym.Target, err)
		}
	}

	// 2. Remove environment directory
	if r.EnvDir != "" {
		if err := os.RemoveAll(r.EnvDir); err != nil {
			fmt.Printf("  warning: could not remove env dir %s: %v\n", r.EnvDir, err)
		}
	}

	// 3. Optionally remove the repo itself
	if removeRepo && r.ClonePath != "" {
		if err := os.RemoveAll(r.ClonePath); err != nil {
			return fmt.Errorf("could not remove repo at %s: %w", r.ClonePath, err)
		}
	}

	// 4. Delete receipt
	return s.Delete(name)
}
