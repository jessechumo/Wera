package scoring

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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
	if outs[0].Analysis == nil || outs[0].Analysis.FitScore != 74 {
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
	s, script := newTestScorer(t, `{"ratings":{"role":8,"seniority":8,"skills":7,"domain":6,"location":7,"blocker":false},"skills_matched":["Linux"],"skills_missing":["Go"],"reason":"Solid."}`)
	years := 2
	s.Facts = map[int64]*Facts{1: {Seniority: "junior", YearsRequired: &years, Sponsorship: "unknown",
		WorkMode: "remote", LocationSummary: "Remote, US", SkillsRequired: []string{"Linux", "Go"}, Digest: "Run Linux fleets."}}
	out := s.Score(context.Background(), testJobs(1))[0]
	if out.Stage != StageScored || out.Analysis.FitScore != 75 || out.Analysis.Verdict != "good" || out.Analysis.Seniority != "junior" ||
		*out.Analysis.YearsRequired != 2 || out.Analysis.WorkMode != "remote" {
		t.Fatalf("merged analysis: stage %q %+v", out.Stage, out.Analysis)
	}
	msgs := script.lastMessages()
	if len(msgs) != 3 || !strings.HasPrefix(msgs[1].Content, "<profile>") || !strings.Contains(msgs[2].Content, "Run Linux fleets.") {
		t.Errorf("compact prompt not used: %+v", msgs)
	}
}

func TestScorerCompactMatchesSkillsInCode(t *testing.T) {
	s, _ := newTestScorer(t, `{"ratings":{"role":8,"seniority":8,"skills":7,"domain":6,"location":7,"blocker":false},"reason":"Fits the Linux work."}`)
	s.Facts = map[int64]*Facts{1: {Seniority: "junior", Sponsorship: "unknown", WorkMode: "remote",
		SkillsRequired: []string{"Linux", "Haskell"}, Digest: "Run Linux fleets."}}
	out := s.Score(context.Background(), testJobs(1))[0]
	if out.Stage != StageScored {
		t.Fatalf("stage %q", out.Stage)
	}
	if len(out.Analysis.SkillsMatched) != 1 || out.Analysis.SkillsMatched[0] != "Linux" ||
		len(out.Analysis.SkillsMissing) != 1 || out.Analysis.SkillsMissing[0] != "Haskell" {
		t.Fatalf("skills: matched %v missing %v", out.Analysis.SkillsMatched, out.Analysis.SkillsMissing)
	}
}

// downProvider answers every call with an error and counts them.
func downProvider(t *testing.T) (*Client, *atomic.Int64) {
	t.Helper()
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(w, "down", http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)
	return NewClient(ClientOptions{BaseURL: srv.URL, APIKey: "k", Model: "m", Backoff: time.Millisecond}), &calls
}

func manyJobs(n int) []Job {
	jobs := make([]Job, n)
	for i := range jobs {
		jobs[i] = Job{ID: int64(i + 1), Company: "Acme", Title: "Engineer", Description: "Build things."}
	}
	return jobs
}

func TestScorerStopsWhenTheProviderIsDown(t *testing.T) {
	client, calls := downProvider(t)
	s := &Scorer{Client: client, Profile: []byte("p"), ProfileHash: "h", Model: "m", Concurrency: 4}
	out := s.Score(context.Background(), manyJobs(500))
	if n := calls.Load(); n > maxCallFailures+8 {
		t.Errorf("kept calling a provider that is down: %d calls for 500 jobs", n)
	}
	for _, o := range out {
		if o.Stage != StageSkipped {
			t.Fatalf("job %d left the queue (%q): a failed call must leave it pending", o.JobID, o.Stage)
		}
	}
}

func TestExtractorStopsWhenTheProviderIsDown(t *testing.T) {
	client, calls := downProvider(t)
	e := &Extractor{Client: client, Model: "m", Concurrency: 4}
	const chunk = 48
	jobs := manyJobs(500)
	for start := 0; start < len(jobs); start += chunk {
		e.Extract(context.Background(), jobs[start:min(start+chunk, len(jobs))])
	}
	if n := calls.Load(); n > maxCallFailures+8 {
		t.Errorf("kept calling a provider that is down: %d calls for 500 jobs", n)
	}
}

func TestOneFailedCallDoesNotStopABatch(t *testing.T) {
	// Every third call fails: a flaky provider, not a dead one.
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1)%3 == 0 {
			http.Error(w, "busy", http.StatusBadGateway)
			return
		}
		w.Write([]byte(`{"choices":[{"message":{"content":"{}"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`))
	}))
	defer srv.Close()
	client := NewClient(ClientOptions{BaseURL: srv.URL, APIKey: "k", Model: "m"})
	s := &Scorer{Client: client, Profile: []byte("p"), ProfileHash: "h", Model: "m", Concurrency: 1}
	s.Score(context.Background(), manyJobs(100))
	if n := calls.Load(); n < 100 {
		t.Errorf("stopped after %d calls", n)
	}
}

func TestRetryCountsTowardTheBudget(t *testing.T) {
	// A reply that is not valid JSON costs money; so does asking again.
	script := &replyScript{replies: []string{"nonsense", "still nonsense"}}
	srv := httptest.NewServer(script)
	defer srv.Close()
	client := NewClient(ClientOptions{BaseURL: srv.URL, APIKey: "k", Model: "m"})
	s := &Scorer{Client: client, Profile: []byte("p"), ProfileHash: "h", Model: "deepseek-v4.1-flash-fast", Concurrency: 1}
	out := s.Score(context.Background(), manyJobs(1))
	if out[0].Usage.PromptTokens != 2000 || out[0].Usage.CompletionTokens != 100 {
		t.Errorf("the retry's tokens are missing: %+v", out[0].Usage)
	}
	if s.Spent() != out[0].CostUSD || s.Spent() <= 0 {
		t.Errorf("spent %v, outcome says %v", s.Spent(), out[0].CostUSD)
	}
}
