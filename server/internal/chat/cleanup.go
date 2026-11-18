package chat

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FileGrace protects fresh files: an upload waits for its message, a drawn
// picture is saved a moment before the reply refers to it.
const (
	FileGrace        = 24 * time.Hour
	PrivateConvTTL   = 2 * time.Hour
	PrivateFileGrace = 15 * time.Minute
)

// DeleteConversation cancels any active generation on the conversation, removes
// the conversation from the store, and immediately unlinks any unreferenced
// files belonging to a private conversation.
func (s *Service) DeleteConversation(ctx context.Context, id, userID string) error {
	if run := s.Hub.ActiveInConversation(id); run != nil && run.UserID == userID {
		run.Cancel(fmt.Errorf("conversation deleted"))
		<-run.Done()
	}
	paths, err := s.Store.DeleteConversationCleanup(ctx, id, userID)
	if err != nil {
		return err
	}
	for _, p := range paths {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			s.Log.Warn("remove private file", "path", p, "err", err)
		}
	}
	return nil
}

// CleanupPrivate purges idle private conversations (along with their messages
// and files) and orphan private uploads that were never sent.
func (s *Service) CleanupPrivate(ctx context.Context, convTTL, fileGrace time.Duration) (int, int, error) {
	convs, err := s.Store.ExpiredPrivateConversations(ctx, time.Now().Add(-convTTL).UnixMilli())
	if err != nil {
		return 0, 0, err
	}
	removedConvs, removedFiles := 0, 0
	for _, c := range convs {
		if ctx.Err() != nil {
			return removedConvs, removedFiles, ctx.Err()
		}
		if s.Hub.ActiveInConversation(c.ID) != nil {
			continue
		}
		paths, err := s.Store.DeleteConversationCleanup(ctx, c.ID, c.UserID)
		if err != nil {
			continue
		}
		removedConvs++
		for _, p := range paths {
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				s.Log.Warn("remove expired private file", "path", p, "err", err)
			} else {
				removedFiles++
			}
		}
	}

	orphans, err := s.Store.OrphanPrivateFiles(ctx, time.Now().Add(-fileGrace).UnixMilli())
	if err != nil {
		return removedConvs, removedFiles, err
	}
	for _, f := range orphans {
		if ctx.Err() != nil {
			return removedConvs, removedFiles, ctx.Err()
		}
		if err := s.Store.DeleteFile(ctx, f.ID); err != nil {
			return removedConvs, removedFiles, err
		}
		if err := os.Remove(f.Path); err != nil && !os.IsNotExist(err) {
			s.Log.Warn("remove orphan private file", "path", f.Path, "err", err)
		}
		removedFiles++
	}
	return removedConvs, removedFiles, nil
}

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
