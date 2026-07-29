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

package main

import (
	"context"
	"errors"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/greenpau/tested/pkg/runner"
)

func TestContextWithSignalCauseRetainsFirstSignal(t *testing.T) {
	notifications := make(chan os.Signal, 2)
	ctx, cancel := contextWithSignalCause(context.Background(), notifications)
	defer cancel()

	notifications <- syscall.SIGTERM
	notifications <- os.Interrupt
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("signal context was not cancelled")
	}

	var cause *runner.SignalCause
	if !errors.As(context.Cause(ctx), &cause) {
		t.Fatalf("context cause = %v, want *runner.SignalCause", context.Cause(ctx))
	}
	if cause.Signal != syscall.SIGTERM {
		t.Fatalf("context signal = %v, want %v", cause.Signal, syscall.SIGTERM)
	}
	if !errors.Is(context.Cause(ctx), context.Canceled) {
		t.Fatalf("context cause = %v, want context cancellation compatibility", cause)
	}
}

func TestContextWithSignalCauseStopsWithoutSignal(t *testing.T) {
	notifications := make(chan os.Signal)
	ctx, cancel := contextWithSignalCause(context.Background(), notifications)
	cancel()

	if !errors.Is(context.Cause(ctx), context.Canceled) {
		t.Fatalf("context cause = %v, want context.Canceled", context.Cause(ctx))
	}
	var cause *runner.SignalCause
	if errors.As(context.Cause(ctx), &cause) {
		t.Fatalf("context cause unexpectedly retained an operator signal: %v", cause)
	}
}
