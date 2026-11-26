package guide

import (
	"strings"
	"testing"
)

func TestAllTopicsEmbeddedAndUnique(t *testing.T) {
	if len(Topics) == 0 {
		t.Fatal("Topics must not be empty")
	}
	seen := map[string]bool{}
	catalog := CatalogMarkdown()
	for _, topic := range Topics {
		if topic.Key == "" || topic.Title == "" || topic.File == "" {
			t.Fatalf("invalid topic definition: %+v", topic)
		}
		if seen[topic.Key] {
			t.Fatalf("duplicate topic key: %q", topic.Key)
		}
		seen[topic.Key] = true
		content, titles, unknown := Read([]string{topic.Key})
		if len(unknown) != 0 || len(titles) != 1 || strings.TrimSpace(content) == "" {
			t.Fatalf("failed to read embedded topic %q: titles=%v unknown=%v len=%d", topic.Key, titles, unknown, len(content))
		}
		if !strings.Contains(catalog, "`"+topic.Key+"`") {
			t.Fatalf("catalog missing topic key %q", topic.Key)
		}
	}
}

func TestReadDeduplicatesCapsAndReportsUnknown(t *testing.T) {
	content, titles, unknown := Read([]string{" Memories ", "memories", "chat", "schedules", "overview", "nonexistent"})
	if len(titles) != MaxTopicsPerRead {
		t.Fatalf("expected %d titles due to cap, got %d (%v)", MaxTopicsPerRead, len(titles), titles)
	}
	if len(unknown) != 1 || unknown[0] != "nonexistent" {
		t.Fatalf("expected unknown=[nonexistent], got %v", unknown)
	}
	if !strings.Contains(content, "30,000") || !strings.Contains(content, "Word") {
		t.Fatalf("memories doc should document 30,000 rune limit and Word/TXT guidance")
	}
}
