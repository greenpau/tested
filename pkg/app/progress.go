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

package app

import (
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/greenpau/tested/pkg/artifact"
	"github.com/greenpau/tested/pkg/result"
)

const progressInterval = 10 * time.Second

type progressConsole interface {
	Stage(string) error
	Event(result.Progress) (bool, error)
	Stderr(string) (bool, error)
}

// liveProgress owns its ticker and serializes stdout/stderr progress. Only the
// first presentation error is retained; display failure never stops capture.
type liveProgress struct {
	mu         sync.Mutex
	console    progressConsole
	enabled    bool
	now        func() time.Time
	phase      string
	started    time.Time
	lastOutput time.Time
	records    uint64
	err        error
	reported   bool
	stop       chan struct{}
	done       chan struct{}
	once       sync.Once
	// repair is installed only after this generation has report inputs. It is
	// called by the orchestrating goroutine, after the ticker has joined.
	repair func()
}

func startLiveProgress(console progressConsole, enabled bool) *liveProgress {
	now := time.Now()
	p := &liveProgress{
		console:    console,
		enabled:    enabled,
		now:        time.Now,
		started:    now,
		lastOutput: now,
		stop:       make(chan struct{}),
		done:       make(chan struct{}),
	}
	if !enabled {
		close(p.done)
		return p
	}
	go func() {
		defer close(p.done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-p.stop:
				return
			case now := <-ticker.C:
				p.heartbeat(now)
			}
		}
	}()
	return p
}

func (p *liveProgress) stage(message string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.phase = message
	if !p.enabled || p.err != nil {
		return
	}
	p.err = p.console.Stage(message)
	p.lastOutput = p.now()
}

func (p *liveProgress) event(update result.Progress) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.records++
	if !p.enabled || p.err != nil {
		return
	}
	written, err := p.console.Event(update)
	p.err = err
	if written {
		p.lastOutput = p.now()
	}
}

func (p *liveProgress) stderr(value string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.enabled || p.err != nil {
		return
	}
	written, err := p.console.Stderr(value)
	p.err = err
	if written {
		p.lastOutput = p.now()
	}
}

func (p *liveProgress) heartbeat(now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.enabled || p.err != nil || now.Sub(p.lastOutput) < progressInterval {
		return
	}
	p.err = p.console.Stage(fmt.Sprintf("Still working: %s — %s elapsed, %d event records", p.phase, now.Sub(p.started).Truncate(time.Second), p.records))
	p.lastOutput = now
}

func (p *liveProgress) collectError(outcome *executionOutcome) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err == nil || p.reported {
		return false
	}
	p.reported = true
	outcome.errors.add(fmt.Errorf("stream live progress: %w", p.err))
	outcome.state.ReportErr = true
	return true
}

func (p *liveProgress) finish(outcome *executionOutcome, layout *artifact.Layout) {
	p.once.Do(func() { close(p.stop) })
	<-p.done
	// Include late errors (even during manifest hashing), without binding them
	// into durable child status or leaving stale successful derivatives.
	if p.collectError(outcome) && p.repair != nil {
		if err := layout.Remove(artifact.ManifestJSON); err != nil {
			outcome.errors.add(fmt.Errorf("remove manifest after progress failure: %w", err))
			outcome.state.InfrastructureErr = true
		}
		p.repair()
	}
}

type progressCapture struct {
	raw      io.Writer
	progress *liveProgress
}

func (w *progressCapture) Write(value []byte) (int, error) {
	n, err := w.raw.Write(value)
	if n > 0 {
		w.progress.stderr(string(value[:n]))
	}
	if err == nil && n != len(value) {
		err = io.ErrShortWrite
	}
	return n, err
}
