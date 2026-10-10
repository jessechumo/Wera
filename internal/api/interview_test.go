package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestInterviewQuiz(t *testing.T) {
	ts, _ := testServer(t, nil)
	_, out := do(t, client, http.MethodGet, ts.URL+"/api/interview/quiz?difficulty=easy&limit=3", "")
	var quiz struct {
		Questions []map[string]any `json:"questions"`
	}
	json.Unmarshal([]byte(out), &quiz)
	if len(quiz.Questions) == 0 {
		t.Fatalf("no questions: %s", out)
	}
	if strings.Contains(out, `"answer"`) || strings.Contains(out, "explanation") {
		t.Error("quiz leaks the answer")
	}
	id := int64(quiz.Questions[0]["id"].(float64))
	code, out := do(t, client, http.MethodPost, fmt.Sprintf("%s/api/interview/questions/%d/answer", ts.URL, id), `{"choice":1}`)
	if code != 200 || !strings.Contains(out, "explanation") {
		t.Fatalf("answer: %d %s", code, out)
	}
	if code, _ := do(t, client, http.MethodPost, fmt.Sprintf("%s/api/interview/questions/%d/answer", ts.URL, id), `{"choice":7}`); code != 400 {
		t.Errorf("bad choice: want 400, got %d", code)
	}
	if _, out := do(t, client, http.MethodGet, ts.URL+"/api/interview/domains", ""); !strings.Contains(out, `"answered":1`) {
		t.Errorf("domain stats: %s", out)
	}
	if code, _ := do(t, client, http.MethodGet, ts.URL+"/api/interview/quiz?difficulty=impossible", ""); code != 400 {
		t.Errorf("bad difficulty: want 400, got %d", code)
	}
}
