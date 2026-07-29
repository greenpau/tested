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

import (
	"errors"
	"testing"
)

func TestCommandWaitStateSerializesSignalAndReapTransition(t *testing.T) {
	state := newCommandWaitState()
	signalErr := errors.New("signal result")
	cleanupErr := errors.New("cleanup result")
	signalEntered := make(chan struct{})
	releaseSignal := make(chan struct{})
	signalFinished := make(chan struct{})

	type signalOutcome struct {
		attempted bool
		err       error
	}
	signalResultCh := make(chan signalOutcome, 1)
	go func() {
		attempted, err := state.signal(func() error {
			close(signalEntered)
			<-releaseSignal
			close(signalFinished)
			return signalErr
		})
		signalResultCh <- signalOutcome{attempted: attempted, err: err}
	}()
	<-signalEntered

	reapStarted := make(chan struct{})
	reapResultCh := make(chan error, 1)
	go func() {
		close(reapStarted)
		reapResultCh <- state.beginReap(func() error {
			select {
			case <-signalFinished:
			default:
				return errors.New("cleanup overtook an in-flight signal")
			}
			return cleanupErr
		})
	}()
	<-reapStarted
	close(releaseSignal)

	signalResult := <-signalResultCh
	if !signalResult.attempted || !errors.Is(signalResult.err, signalErr) {
		t.Fatalf(
			"signal result = (%t, %v), want attempted signal error",
			signalResult.attempted,
			signalResult.err,
		)
	}
	if err := <-reapResultCh; !errors.Is(err, cleanupErr) {
		t.Fatalf("beginReap() error = %v, want cleanup error", err)
	}
	select {
	case <-state.startedReaping():
	default:
		t.Fatal("beginReap() did not publish the transition")
	}

	operationCalled := false
	attempted, err := state.signal(func() error {
		operationCalled = true
		return errors.New("post-reap signal")
	})
	if attempted || err != nil || operationCalled {
		t.Fatalf(
			"post-reap signal = (%t, %v, called %t), want skipped",
			attempted,
			err,
			operationCalled,
		)
	}
}
