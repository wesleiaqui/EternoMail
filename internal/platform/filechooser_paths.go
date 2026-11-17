package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// portalSaveNames reserves a different safe basename for every MIME occurrence.
func portalSaveNames(names []string) []string {
	result := make([]string, len(names))
	used := make(map[string]bool, len(names))
	for i, name := range names {
		name = filepath.Base(strings.ReplaceAll(strings.ReplaceAll(name, "\\", "/"), "\x00", ""))
		if name == "" || name == "." || name == ".." || name == "/" {
			name = "attachment"
		}
		ext := filepath.Ext(name)
		stem := strings.TrimSuffix(name, ext)
		candidate := name
		for n := 2; used[strings.ToLower(candidate)]; n++ {
			candidate = fmt.Sprintf("%s (%d)%s", stem, n, ext)
		}
		result[i] = candidate
		used[strings.ToLower(candidate)] = true
	}
	return result
}

// validatePortalSavePaths rejects ambiguous results before any part is written.
// Some portal implementations may return the same path for repeated filenames.
func validatePortalSavePaths(paths []string, count int) error {
	if len(paths) != count {
		return fmt.Errorf("portal returned %d destinations for %d attachments", len(paths), count)
	}
	seen := make(map[string]bool, len(paths))
	existing := make([]os.FileInfo, 0, len(paths))
	for _, path := range paths {
		if path == "" || !filepath.IsAbs(path) {
			return fmt.Errorf("portal returned an invalid destination")
		}
		key := filepath.Clean(path)
		resolved, resolveErr := filepath.EvalSymlinks(key)
		if resolveErr == nil {
			key = resolved
		}
		if resolveErr != nil {
			if parent, err := filepath.EvalSymlinks(filepath.Dir(key)); err == nil {
				key = filepath.Join(parent, filepath.Base(key))
			}
		}
		if seen[key] {
			return fmt.Errorf("portal returned duplicate attachment destinations")
		}
		seen[key] = true
		info, err := os.Stat(path)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("check portal destination: %w", err)
		}
		if err != nil {
			continue
		}
		for _, prior := range existing {
			if os.SameFile(info, prior) {
				return fmt.Errorf("portal destinations refer to the same file")
			}
		}
		existing = append(existing, info)
	}
	return nil
}
