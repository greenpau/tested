// Copyright 2026 Paul Greenberg greenpau@outlook.com
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package runner

import "sync"

type commandWaitResult struct {
	waitErr    error
	cleanupErr error
}

// commandWaitState serializes process-tree signaling with the transition to
// reaping. On platforms with a non-reaping exit observer, beginReap is called
// while the leader PID still belongs to the child. No later signal operation
// can therefore address a recycled numeric process-group identifier.
type commandWaitState struct {
	mu          sync.Mutex
	signalable  bool
	reapStarted chan struct{}
}

func newCommandWaitState() *commandWaitState {
	return &commandWaitState{
		signalable:  true,
		reapStarted: make(chan struct{}),
	}
}

func (s *commandWaitState) signal(operation func() error) (bool, error) {
	if s == nil || operation == nil {
		return false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.signalable {
		return false, nil
	}
	return true, operation()
}

func (s *commandWaitState) beginReap(cleanup func() error) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.signalable {
		return nil
	}
	var cleanupErr error
	if cleanup != nil {
		cleanupErr = cleanup()
	}
	s.signalable = false
	close(s.reapStarted)
	return cleanupErr
}

func (s *commandWaitState) startedReaping() <-chan struct{} {
	if s == nil {
		return nil
	}
	return s.reapStarted
}
