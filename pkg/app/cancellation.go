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
	"context"

	"github.com/greenpau/tested/pkg/runner"
)

func mergeContextCancellation(
	ctx context.Context,
	runResult *runner.Result,
) bool {
	if runResult == nil || runResult.Cancellation != runner.CancellationNone {
		return false
	}
	details := runner.CancellationResult(ctx)
	if details.Kind == runner.CancellationNone {
		return false
	}
	runResult.Interrupted = true
	runResult.Cancellation = details.Kind
	runResult.CancellationSignal = details.Signal
	runResult.RecommendedExitCode = details.RecommendedExitCode
	return true
}

func applyContextCancellation(
	ctx context.Context,
	state *ExitState,
) bool {
	if state == nil {
		return false
	}
	details := runner.CancellationResult(ctx)
	if details.Kind == runner.CancellationNone {
		return false
	}
	state.Interrupted = true
	state.InterruptExitCode = details.RecommendedExitCode
	return true
}
