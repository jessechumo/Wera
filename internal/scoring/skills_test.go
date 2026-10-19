package scoring

import (
	"reflect"
	"testing"
)

func TestMatchSkills(t *testing.T) {
	profile := NewSkillSet("Python, PyTorch, C++ and Kubernetes. Built distributed training on AWS. Machine learning research.")
	matched, missing := MatchSkills(profile, []string{
		"Python", "C++", "Distributed systems", "Machine Learning", "Go", "Kubernetes (K8s)", "Software engineering", "", "Rust",
	})
	if want := []string{"Python", "C++", "Distributed systems", "Machine Learning", "Kubernetes (K8s)"}; !reflect.DeepEqual(matched, want) {
		t.Errorf("matched = %v, want %v", matched, want)
	}
	if want := []string{"Go", "Software engineering", "Rust"}; !reflect.DeepEqual(missing, want) {
		t.Errorf("missing = %v, want %v", missing, want)
	}
	if m, x := MatchSkills(profile, nil); len(m) != 0 || len(x) != 0 || m == nil || x == nil {
		t.Errorf("no skills should give two empty lists, got %v %v", m, x)
	}
}
