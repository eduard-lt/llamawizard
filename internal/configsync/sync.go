// Package configsync plans and applies non-destructive configuration updates.
package configsync

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/eduard-lt/llamawizard/internal/atomicfile"
	"github.com/eduard-lt/llamawizard/internal/hardware"
	"github.com/eduard-lt/llamawizard/internal/llamaswap"
	"github.com/eduard-lt/llamawizard/internal/pi"
	"github.com/eduard-lt/llamawizard/internal/state"
)

type Options struct {
	// ApplyOnly synchronizes the master as-is; add/remove operations also merge inventory.
	ApplyOnly    bool
	Remove       []string
	APIKey       *string
	DefaultModel string
}
type Change struct {
	Path          string
	Before, After []byte
	Exists        bool
	Mode          os.FileMode
}
type Plan struct {
	Master     []byte
	Changes    []Change
	Guards     []Change
	Models     []state.ModelEntry
	APIKey     string
	Port       int
	backupRoot string
}

func read(path string) ([]byte, bool, os.FileMode, error) {
	st, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, false, 0o600, nil
	}
	if err != nil {
		return nil, false, 0, err
	}
	if !st.Mode().IsRegular() {
		return nil, false, 0, fmt.Errorf("refusing non-regular config file %s", path)
	}
	b, err := os.ReadFile(path)
	return b, true, st.Mode().Perm(), err
}
func (p *Plan) add(path string, data []byte) error {
	before, exists, mode, err := read(path)
	if err != nil {
		return err
	}
	p.Guards = append(p.Guards, Change{path, before, data, exists, mode})
	if !bytes.Equal(before, data) {
		p.Changes = append(p.Changes, Change{path, before, data, exists, mode})
	}
	return nil
}
func Prepare(st *state.State, opts Options) (*Plan, error) {
	if st.Port < 1 || st.Port > 65535 {
		return nil, fmt.Errorf("invalid service port %d", st.Port)
	}
	master, _, _, err := read(state.DefaultConfigPath())
	if err != nil {
		return nil, err
	}
	sourceMaster := append([]byte(nil), master...)
	original := append([]byte(nil), master...)
	snapshotPath := filepath.Join(filepath.Dir(state.DefaultPath()), "last-applied.yaml")
	snapshot, _, _, err := read(snapshotPath)
	if err != nil {
		return nil, err
	}
	if len(snapshot) > 0 {
		original = snapshot
	}
	if !opts.ApplyOnly {
		// Generate only absent models. Existing commands may point to a different
		// executable and never need regeneration or hardware recalculation.
		present := map[string]bool{}
		if len(master) > 0 {
			entries, _, err := llamaswap.Catalog(master)
			if err != nil {
				return nil, err
			}
			for _, m := range entries {
				present[m.Slug] = true
			}
		}
		previous := map[string]bool{}
		if len(snapshot) > 0 {
			entries, _, err := llamaswap.Catalog(snapshot)
			if err != nil {
				return nil, err
			}
			for _, m := range entries {
				previous[m.Slug] = true
			}
		}
		var missing []state.ModelEntry
		for _, m := range st.Models {
			if !present[m.Slug] && !previous[m.Slug] {
				missing = append(missing, m)
			}
		}
		generated := []byte("models: {}\n")
		if len(missing) > 0 || len(master) == 0 {
			path := st.LlamaCppPath
			if len(missing) == 0 && path == "" {
				path = "/usr/bin/true"
			}
			hw, _ := hardware.Detect()
			generated, err = llamaswap.GenerateConfig(missing, st.APIKey, path, hw)
			if err != nil {
				return nil, err
			}
		}
		master, err = llamaswap.Merge(master, generated, opts.Remove, opts.APIKey)
		if err != nil {
			return nil, err
		}
	} else if len(master) == 0 {
		return nil, fmt.Errorf("master config not found; run setup first")
	}
	models, key, err := llamaswap.Catalog(master)
	if err != nil {
		return nil, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	p := &Plan{Master: master, Models: models, APIKey: key, Port: st.Port, backupRoot: filepath.Join(home, ".local", "ai", "backups")}
	if err = p.add(state.DefaultConfigPath(), master); err != nil {
		return nil, err
	}
	if st.PiConfigured {
		// Alias removals must propagate too. Pi-only entries are never inferred stale.
		removed := append([]string(nil), opts.Remove...)
		if len(original) > 0 {
			old, _, err := llamaswap.Catalog(original)
			if err != nil {
				return nil, err
			}
			current := map[string]bool{}
			for _, m := range models {
				current[m.Slug] = true
			}
			for _, m := range old {
				if !current[m.Slug] {
					removed = append(removed, m.Slug)
				}
			}
		}
		dir := filepath.Join(home, ".pi", "agent")
		mp := filepath.Join(dir, "models.json")
		sp := filepath.Join(dir, "settings.json")
		before, _, _, err := read(mp)
		if err != nil {
			return nil, err
		}
		data, err := pi.RenderModels(before, st.Port, key, models, removed)
		if err != nil {
			return nil, fmt.Errorf("pi models: %w", err)
		}
		if err = p.add(mp, data); err != nil {
			return nil, err
		}
		before, _, _, err = read(sp)
		if err != nil {
			return nil, err
		}
		data, err = pi.RenderSettings(before, opts.DefaultModel, models)
		if err != nil {
			return nil, fmt.Errorf("pi settings: %w", err)
		}
		if err = p.add(sp, data); err != nil {
			return nil, err
		}
	}
	if err = p.add(snapshotPath, master); err != nil {
		return nil, err
	}
	next := *st
	next.APIKey = key
	next.SchemaVersion = 1
	data, err := json.MarshalIndent(&next, "", "  ")
	if err != nil {
		return nil, err
	}
	if err = p.add(state.DefaultPath(), data); err != nil {
		return nil, err
	}
	current, _, _, err := read(state.DefaultConfigPath())
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(sourceMaster, current) {
		return nil, fmt.Errorf("master changed during planning; retry")
	}
	return p, nil
}

var replaceFile = atomicfile.Write

// Commit backs up all changed files before replacing any, checks concurrent
// edits, and rolls back on a write failure. Backups survive failed commits.
func (p *Plan) Commit() (string, error) {
	if len(p.Changes) == 0 {
		return "", nil
	}
	root := filepath.Dir(p.backupRoot)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	lock := filepath.Join(root, "config.lock")
	f, err := os.OpenFile(lock, os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return "", fmt.Errorf("another config update is running: %w", err)
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()

	for _, c := range p.Guards {
		data, exists, _, err := read(c.Path)
		if err != nil {
			return "", err
		}
		if exists != c.Exists || !bytes.Equal(data, c.Before) {
			return "", fmt.Errorf("%s changed during planning; retry", c.Path)
		}
	}
	if err = os.MkdirAll(p.backupRoot, 0o700); err != nil {
		return "", err
	}
	backup, err := os.MkdirTemp(p.backupRoot, time.Now().Format("20060102-150405-"))
	if err != nil {
		return "", err
	}
	for i, c := range p.Changes {
		if c.Exists {
			if err = atomicfile.Write(filepath.Join(backup, fmt.Sprintf("%02d-%s", i, filepath.Base(c.Path))), c.Before, 0o600); err != nil {
				return backup, err
			}
		}
	}
	manifest, _ := json.MarshalIndent(p.Changes, "", "  ")
	if err = atomicfile.Write(filepath.Join(backup, "manifest.json"), manifest, 0o600); err != nil {
		return backup, err
	}
	for i, c := range p.Changes {
		if err = replaceFile(c.Path, c.After, 0o600); err != nil {
			var rollbackErr error
			for j := i - 1; j >= 0; j-- {
				old := p.Changes[j]
				var e error
				if old.Exists {
					e = atomicfile.Write(old.Path, old.Before, old.Mode)
				} else {
					e = os.Remove(old.Path)
				}
				if e != nil {
					rollbackErr = e
				}
			}
			if rollbackErr != nil {
				return backup, fmt.Errorf("apply failed: %v; rollback failed: %v; backups: %s", err, rollbackErr, backup)
			}
			return backup, err
		}
	}
	return backup, nil
}
