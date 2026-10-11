package api

import (
	"errors"
	"testing"
	"time"
)

func TestTTLCache(t *testing.T) {
	c := newTTLCache(50*time.Millisecond, 2)
	calls := 0
	get := func(k string) any {
		v, _ := c.get(k, func() (any, error) { calls++; return k + "!", nil })
		return v
	}
	first, second := get("a"), get("a")
	if first != "a!" || second != "a!" || calls != 1 {
		t.Fatalf("not cached: %d calls", calls)
	}
	if _, err := c.get("e", func() (any, error) { return nil, errors.New("boom") }); err == nil {
		t.Fatal("error lost")
	}
	if _, err := c.get("e", func() (any, error) { calls++; return 1, nil }); err != nil || calls != 2 {
		t.Fatal("errors must not be cached")
	}
	get("b") // full: a and e fill it, so it resets
	time.Sleep(60 * time.Millisecond)
	get("a")
	if calls != 4 {
		t.Errorf("expired entry served: %d calls", calls)
	}
}
