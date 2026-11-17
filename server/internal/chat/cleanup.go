package chat

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FileGrace protects fresh files: an upload waits for its message, a drawn
// picture is saved a moment before the reply refers to it.
const FileGrace = 24 * time.Hour

// CleanupFiles deletes files no message refers to any more (deleted
// conversations, edited or regenerated messages, uploads never sent) and
// files on disk without a row (users deleted with their files), once they
// are older than grace (FileGrace in production). It returns how many files were removed.
func (s *Service) CleanupFiles(ctx context.Context, grace time.Duration) (int, error) {
	cutoff := time.Now().Add(-grace)
	orphans, err := s.Store.OrphanFiles(ctx, cutoff.UnixMilli())
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, f := range orphans {
		if err := s.Store.DeleteFile(ctx, f.ID); err != nil {
			return removed, err
		}
		if err := os.Remove(f.Path); err != nil && !os.IsNotExist(err) {
			s.Log.Warn("remove file", "path", f.Path, "err", err)
		}
		removed++
	}

	ids, err := s.Store.FileIDs(ctx)
	if err != nil {
		return removed, err
	}
	root := filepath.Join(s.DataDir, "files")
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return filepath.SkipAll
			}
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			return nil
		}
		id := strings.TrimSuffix(d.Name(), filepath.Ext(d.Name()))
		if _, ok := ids[id]; ok {
			return nil
		}
		if info, err := d.Info(); err != nil || info.ModTime().After(cutoff) {
			return nil
		}
		if err := os.Remove(path); err == nil {
			removed++
		}
		return nil
	})
	return removed, err
}
