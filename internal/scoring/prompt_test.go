package scoring

import (
	"strings"
	"testing"
)

const testProfile = "# Jesse\n- Linux, Python, some K8s\n"

func TestProfileHashAndCacheKey(t *testing.T) {
	h1 := ProfileHash([]byte(testProfile))
	h2 := ProfileHash([]byte(testProfile))
	if h1 != h2 {
		t.Fatal("hash not deterministic")
	}
	if len(h1) != 64 {
		t.Errorf("want 64 hex chars, got %d", len(h1))
	}
	key := CacheKey(h1)
	if !strings.HasPrefix(key, "wera-profile-") {
		t.Errorf("cache key prefix: %q", key)
	}
	if want := "wera-profile-" + h1[:16]; key != want {
		t.Errorf("cache key: got %q, want %q", key, want)
	}
}

func TestBuildMessagesIdenticalPrefix(t *testing.T) {
	jobA := Job{ID: 1, Company: "DRW", Title: "SRE I", Location: "Chicago", URL: "u", Description: "Run things."}
	jobB := Job{ID: 2, Company: "Baseten", Title: "Infra Engineer", Location: "SF", URL: "v", Description: "Ship things."}

	ma := BuildMessages([]byte(testProfile), jobA)
	mb := BuildMessages([]byte(testProfile), jobB)

	if len(ma) != 3 || len(mb) != 3 {
		t.Fatalf("want 3 messages, got %d/%d", len(ma), len(mb))
	}
	if ma[0].Role != "system" || ma[1].Role != "user" || ma[2].Role != "user" {
		t.Errorf("roles: %v %v %v", ma[0].Role, ma[1].Role, ma[2].Role)
	}
	// The prefix (system + profile) must be byte-identical for caching.
	if ma[0].Content != mb[0].Content || ma[1].Content != mb[1].Content {
		t.Error("prefix messages differ between jobs; caching would break")
	}
	if ma[2].Content == mb[2].Content {
		t.Error("job messages should differ")
	}
}

func TestBuildMessagesTruncatesDescription(t *testing.T) {
	long := strings.Repeat("x", maxDescriptionChars+500)
	msgs := BuildMessages([]byte(testProfile), Job{Description: long})
	jobPart := msgs[2].Content
	if !strings.HasSuffix(jobPart, strings.Repeat("x", maxDescriptionChars)) {
		t.Errorf("description should end with exactly %d x's", maxDescriptionChars)
	}
	if strings.Contains(jobPart, strings.Repeat("x", maxDescriptionChars+1)) {
		t.Errorf("description not truncated to %d chars", maxDescriptionChars)
	}
}
