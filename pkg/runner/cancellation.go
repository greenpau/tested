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
	"context"
	"errors"
	"fmt"
	"os"
)

// CancellationKind identifies why tested stopped an owned child process.
type CancellationKind string

const (
	// CancellationNone means the runner did not take ownership of cancellation.
	CancellationNone CancellationKind = ""
	// CancellationProgrammatic identifies context cancellation without a
	// deadline or operator signal.
	CancellationProgrammatic CancellationKind = "programmatic"
	// CancellationDeadline identifies an expired context deadline.
	CancellationDeadline CancellationKind = "deadline"
	// CancellationSignal identifies an operator signal retained in the context
	// as a SignalCause.
	CancellationSignal CancellationKind = "signal"
)

// SignalCause retains the operating-system signal that cancelled a context.
// Use NewSignalCause as the cause passed to context.CancelCauseFunc.
type SignalCause struct {
	Signal os.Signal
}

// NewSignalCause creates a typed context cancellation cause for signal.
func NewSignalCause(signal os.Signal) *SignalCause {
	return &SignalCause{Signal: signal}
}

// Error implements error.
func (c *SignalCause) Error() string {
	if c == nil || c.Signal == nil {
		return "cancelled by operator signal"
	}
	return fmt.Sprintf("cancelled by operator signal %s", c.Signal)
}

// Unwrap retains compatibility with callers checking context.Canceled while
// allowing context.Cause to expose the exact operator signal.
func (c *SignalCause) Unwrap() error {
	return context.Canceled
}

type cancellationDetails struct {
	kind                CancellationKind
	signal              os.Signal
	signalName          string
	recommendedExitCode int
}

// CancellationDetails is a read-only projection of a context cancellation
// cause for application phases that run before or after an owned child.
type CancellationDetails struct {
	Kind                CancellationKind
	Signal              string
	RecommendedExitCode int
}

// CancellationResult classifies ctx without changing or cancelling it. An
// active or nil context returns zero-value details.
func CancellationResult(ctx context.Context) CancellationDetails {
	details := cancellationFromContext(ctx)
	return CancellationDetails{
		Kind:                details.kind,
		Signal:              details.signalName,
		RecommendedExitCode: details.recommendedExitCode,
	}
}

func cancellationFromContext(ctx context.Context) cancellationDetails {
	if ctx == nil || ctx.Err() == nil {
		return cancellationDetails{kind: CancellationNone}
	}
	details := cancellationDetails{
		kind:                CancellationProgrammatic,
		signal:              os.Interrupt,
		recommendedExitCode: recommendedSignalExitCode(os.Interrupt),
	}
	cause := context.Cause(ctx)
	var signalCause *SignalCause
	if errors.As(cause, &signalCause) && signalCause != nil &&
		signalCause.Signal != nil {
		details.kind = CancellationSignal
		details.signal = signalCause.Signal
		details.signalName = signalCause.Signal.String()
		details.recommendedExitCode =
			recommendedSignalExitCode(signalCause.Signal)
		return details
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		details.kind = CancellationDeadline
	}
	return details
}

func applyCancellation(result *Result, details cancellationDetails) {
	result.Interrupted = true
	result.Cancellation = details.kind
	result.CancellationSignal = details.signalName
	result.RecommendedExitCode = details.recommendedExitCode
}
