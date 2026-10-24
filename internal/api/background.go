package api

import "runtime/debug"

// background runs fn on its own goroutine, after the request that started
// it has returned (scoring an added job, importing a resume, matching, a
// pipeline run). The router recovers only its own goroutines: without
// this, a panic in such a task would take the whole server down.
func (s *Server) background(task string, fn func()) {
	go func() {
		defer func() {
			if v := recover(); v != nil {
				s.Log.Error("background task panicked", "task", task, "panic", v, "stack", string(debug.Stack()))
			}
		}()
		fn()
	}()
}
