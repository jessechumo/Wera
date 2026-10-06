package main

import (
	"reflect"
	"testing"
)

func TestCandidateSlugs(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		provided []string
		want     []string
	}{
		{"spec example", "Fireworks AI", nil,
			[]string{"fireworksai", "fireworks-ai", "fireworks"}},
		{"explicit slug first", "Groq", []string{"groq"},
			[]string{"groq"}},
		{"explicit slug plus derived", "Fireworks AI", []string{"fireworks"},
			[]string{"fireworks", "fireworksai", "fireworks-ai"}},
		{"multi word", "Hudson River Trading", nil,
			[]string{"hudsonrivertrading", "hudson-river-trading", "hudsonriver", "hudson-river"}},
		{"single word", "Baseten", nil, []string{"baseten"}},
		{"punctuation and suffix stripped", "Two Sigma, Inc.", nil,
			[]string{"twosigmainc", "two-sigma-inc", "twosigma", "two-sigma"}},
		{"suffix chain", "Lambda Labs Inc", nil,
			[]string{"lambdalabsinc", "lambda-labs-inc", "lambdalabs", "lambda-labs", "lambda"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := candidateSlugs(tc.in, tc.provided...)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("candidateSlugs(%q, %v)\n got: %v\nwant: %v", tc.in, tc.provided, got, tc.want)
			}
		})
	}
}
