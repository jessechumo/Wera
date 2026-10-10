package scoring

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// replyScript serves queued reply bodies, one per request, recording every
// request's messages so tests can inspect the cached prefix.
type replyScript struct {
	mu       sync.Mutex
	replies  []string
	messages [][]Message
}

func (s *replyScript) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req request
	if err := jsonDecode(r, &req); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.messages = append(s.messages, req.Messages)
	if len(s.messages) > len(s.replies) {
		s.mu.Unlock()
		http.Error(w, "unexpected extra request", http.StatusTooManyRequests)
		return
	}
	reply := s.replies[len(s.messages)-1]
	s.mu.Unlock()

	content, _ := json.Marshal(reply)
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"choices":[{"message":{"content":` + string(content) + `}}],"usage":{"prompt_tokens":1000,"completion_tokens":50,"prompt_tokens_details":{"cached_tokens":900}}}`))
}

func (s *replyScript) lastMessages() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.messages) == 0 {
		return nil
	}
	return s.messages[len(s.messages)-1]
}

func (s *replyScript) requestCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.messages)
}

func newTestScorer(t *testing.T, replies ...string) (*Scorer, *replyScript) {
	t.Helper()
	script := &replyScript{replies: replies}
	srv := httptest.NewServer(script)
	t.Cleanup(srv.Close)
	client := NewClient(ClientOptions{BaseURL: srv.URL, APIKey: "cb_test", Model: "glm-5.3-flash-fast"})
	s := &Scorer{
		Client:      client,
		Profile:     []byte(testProfile),
		ProfileHash: ProfileHash([]byte(testProfile)),
		Model:       "glm-5.3-flash-fast",
		Rules:       Exclusions{MaxYears: 3, RequireSponsorship: true, USOnly: true},
		Concurrency: 1,
	}
	return s, script
}

func testJobs(n int) []Job {
	jobs := make([]Job, n)
	for i := range jobs {
		jobs[i] = Job{ID: int64(i + 1), Company: "TestCo", Title: "SRE I",
			Location: "Chicago, IL", URL: "https://example.com", Description: "Run Linux."}
	}
	return jobs
}

func TestScorerScoresValidReply(t *testing.T) {
	s, script := newTestScorer(t, validReply)
	outs := s.Score(context.Background(), testJobs(1))
	if len(outs) != 1 {
		t.Fatalf("want 1 outcome, got %d", len(outs))
	}
	if outs[0].Stage != StageScored {
		t.Errorf("stage: got %q, want scored (err=%v)", outs[0].Stage, outs[0].Err)
	}
	if outs[0].Analysis == nil || outs[0].Analysis.FitScore != 72 {
		t.Errorf("analysis: %+v", outs[0].Analysis)
	}
	// Cost from the fake usage: uncached 100 prompt x 0.15 + 50 x 0.50.
	wantCost := (100*0.15 + 50*0.50) / 1e6
	if outs[0].CostUSD != wantCost {
		t.Errorf("cost: got %.9f, want %.9f", outs[0].CostUSD, wantCost)
	}
	// The request's cached prefix must be byte-identical to BuildMessages.
	msgs := script.messages[0]
	want := BuildMessages([]byte(testProfile), testJobs(1)[0])
	if len(msgs) != len(want) {
		t.Fatalf("message count: got %d", len(msgs))
	}
	for i := range msgs {
		if msgs[i] != want[i] {
			t.Errorf("message %d differs from BuildMessages", i)
		}
	}
}

func TestScorerRetriesInvalidJSONOnce(t *testing.T) {
	s, script := newTestScorer(t, "sorry, I cannot answer in JSON", validReply)
	outs := s.Score(context.Background(), testJobs(1))
	if outs[0].Stage != StageScored {
		t.Errorf("stage: got %q, want scored after retry", outs[0].Stage)
	}
	if n := script.requestCount(); n != 2 {
		t.Errorf("want 2 requests (invalid + retry), got %d", n)
	}
	// The retry must append the note after the original messages.
	retryMsgs := script.messages[1]
	if len(retryMsgs) != 4 || retryMsgs[3].Content != retryNote || retryMsgs[3].Role != "user" {
		t.Errorf("retry messages: %+v", retryMsgs)
	}
	// Prefix still intact on the retry.
	if retryMsgs[0].Content != systemPrompt || retryMsgs[1].Content != "<profile>\n"+testProfile+"\n</profile>" {
		t.Error("retry broke the cached prefix")
	}
}

func TestScorerScoreFailedAfterTwoBadReplies(t *testing.T) {
	s, _ := newTestScorer(t, "total garbage", "still garbage")
	outs := s.Score(context.Background(), testJobs(1))
	if outs[0].Stage != StageFailed {
		t.Errorf("stage: got %q, want score_failed", outs[0].Stage)
	}
	if outs[0].Raw != "still garbage" {
		t.Errorf("raw: got %q", outs[0].Raw)
	}
}

func TestScorerStreamsOutcomesInOrder(t *testing.T) {
	s, _ := newTestScorer(t, validReply, validReply, validReply)
	var got []int64
	var mu sync.Mutex
	s.OnOutcome = func(o Outcome) {
		mu.Lock()
		got = append(got, o.JobID)
		mu.Unlock()
	}
	outs := s.Score(context.Background(), testJobs(3)) // Concurrency 1
	if len(outs) != 3 || len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Fatalf("streamed %v, want 1,2,3 in order", got)
	}
}

func TestScorerCompactPathUsesFacts(t *testing.T) {
	s, script := newTestScorer(t, `{"fit_score":77,"verdict":"good","skills_matched":["Linux"],"skills_missing":["Go"],"reason":"Solid."}`)
	years := 2
	s.Facts = map[int64]*Facts{1: {Seniority: "junior", YearsRequired: &years, Sponsorship: "unknown",
		WorkMode: "remote", LocationSummary: "Remote, US", SkillsRequired: []string{"Linux", "Go"}, Digest: "Run Linux fleets."}}
	out := s.Score(context.Background(), testJobs(1))[0]
	if out.Stage != StageScored || out.Analysis.FitScore != 77 || out.Analysis.Seniority != "junior" ||
		*out.Analysis.YearsRequired != 2 || out.Analysis.WorkMode != "remote" {
		t.Fatalf("merged analysis: stage %q %+v", out.Stage, out.Analysis)
	}
	msgs := script.lastMessages()
	if len(msgs) != 3 || !strings.HasPrefix(msgs[1].Content, "<profile>") || !strings.Contains(msgs[2].Content, "Run Linux fleets.") {
		t.Errorf("compact prompt not used: %+v", msgs)
	}
}
