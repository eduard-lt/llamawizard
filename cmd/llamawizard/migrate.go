package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/eduard-lt/llamawizard/internal/pi"
	"github.com/eduard-lt/llamawizard/internal/state"
)

// migrateSlugs normalizes model slugs so each includes its quantization suffix
// (e.g. "qwen3.8-27b" → "qwen3.8-27b-q5-k-xl"). This guarantees different
// quantizations of the same model get distinct slugs, then moves files into
// per-quant directories and rewrites the config and pi configuration. It is a
// no-op when every slug already carries its quant.
func migrateSlugs() error {
	st, err := state.Load("")
	if err != nil {
		return err
	}
	if len(st.Models) == 0 {
		return nil
	}

	piActive := pi.IsInstalled() || st.PiConfigured
	oldDefault := ""
	if piActive {
		oldDefault = pi.CurrentDefaultModel()
	}

	// used tracks slugs currently occupied, so renames never collide.
	used := map[string]bool{}
	for i := range st.Models {
		used[st.Models[i].Slug] = true
	}

	changed := false
	oldToNew := map[string]string{}
	dirsToRemove := map[string]bool{}

	for i := range st.Models {
		m := &st.Models[i]
		target := normalizeSlug(m.Slug, m.Quant)
		if target == m.Slug {
			continue
		}
		if used[target] {
			target = uniqueSlug(target, used)
		}
		if err := moveModelFiles(m.Slug, target, m.File, m.Mmproj); err != nil {
			return fmt.Errorf("moving files for %s: %w", m.Slug, err)
		}
		oldToNew[m.Slug] = target
		used[target] = true
		dirsToRemove[m.Slug] = true
		m.Slug = target
		changed = true
	}

	// Remove source directories after all files have been moved out. Safe for
	// shared dirs (two quants that previously shared a slug) because
	// moveModelFiles hardlinks the mmproj into each target directory.
	for slug := range dirsToRemove {
		home, _ := os.UserHomeDir()
		_ = os.RemoveAll(filepath.Join(home, "models", slug))
	}

	// Reconfigure pi when slugs changed, or when pi's default model references
	// a slug that no longer exists (e.g. one renamed by a prior run).
	needsPi := changed && piActive
	if piActive && !needsPi && oldDefault != "" && !hasSlug(st.Models, oldDefault) {
		needsPi = true
	}

	if changed {
		if err := st.Save(""); err != nil {
			return err
		}
		fmt.Println("Normalized model slugs (quant suffixes added).")
		regenerateConfig(st)
	}

	if needsPi {
		newDefault := oldToNew[oldDefault]
		if newDefault == "" && len(st.Models) > 0 {
			newDefault = st.Models[0].Slug
		}
		if err := pi.ConfigureModels(st.Port, st.Models); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: pi model config failed: %v\n", err)
		} else if err := pi.ConfigureSettings(newDefault, st.Models); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: pi settings failed: %v\n", err)
		} else {
			fmt.Printf("pi configuration updated (default model: %s).\n", newDefault)
		}
	}

	if changed {
		restartServiceAfterAdd()
	}
	return nil
}

// hasSlug reports whether any model uses the given slug.
func hasSlug(models []state.ModelEntry, slug string) bool {
	for _, m := range models {
		if m.Slug == slug {
			return true
		}
	}
	return false
}

// normalizeSlug ensures a slug ends with the sanitized quant, so different
// quantizations of the same model never collide. Slugs that already carry the
// quant are left untouched, as are models without a real quant (empty or
// "custom").
func normalizeSlug(slug, quant string) string {
	q := slugFromName(quant)
	if q == "" || q == "custom" {
		return slug
	}
	if strings.HasSuffix(slug, "-"+q) {
		return slug
	}
	return slug + "-" + q
}

// uniqueSlug returns base if unused, otherwise base-2, base-3, ...
func uniqueSlug(base string, used map[string]bool) string {
	if !used[base] {
		return base
	}
	for n := 2; ; n++ {
		cand := fmt.Sprintf("%s-%d", base, n)
		if !used[cand] {
			return cand
		}
	}
}

// moveModelFiles moves a single model's file (and mmproj) from the old slug
// directory to the new one. The mmproj is hardlinked (copied as a fallback) so
// a shared projector can live in multiple directories.
func moveModelFiles(oldSlug, newSlug, file, mmproj string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	oldDir := filepath.Join(home, "models", oldSlug)
	newDir := filepath.Join(home, "models", newSlug)
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		return err
	}

	if file != "" {
		oldPath := filepath.Join(oldDir, file)
		newPath := filepath.Join(newDir, file)
		if _, err := os.Stat(oldPath); err == nil {
			if err := os.Rename(oldPath, newPath); err != nil {
				return err
			}
		}
	}

	if mmproj != "" {
		oldPath := filepath.Join(oldDir, mmproj)
		newPath := filepath.Join(newDir, mmproj)
		if _, err := os.Stat(oldPath); err == nil {
			if _, err := os.Stat(newPath); os.IsNotExist(err) {
				if err := os.Link(oldPath, newPath); err != nil {
					if err := copyFile(oldPath, newPath); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
