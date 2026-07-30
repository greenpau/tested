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

package fixture

import (
	"os"
	"strings"
	"testing"
)

func TestSum(t *testing.T) {
	t.Run("positive", func(t *testing.T) {
		t.Parallel()
		if got := Sum(2, 3); got != 5 {
			t.Fatalf("Sum(2, 3) = %d, want 5", got)
		}
	})
	t.Run("negative", func(t *testing.T) {
		t.Parallel()
		if got := Sum(-2, -3); got != -5 {
			t.Fatalf("Sum(-2, -3) = %d, want -5", got)
		}
	})
}

func TestSkipped(t *testing.T) {
	t.Skip("intentional fixture skip")
}

func TestControlledFailure(t *testing.T) {
	if os.Getenv("TESTED_FIXTURE_FAIL") == "1" {
		t.Fatal("intentional fixture failure")
	}
}

func TestLargeOutput(t *testing.T) {
	if os.Getenv("TESTED_FIXTURE_LARGE") != "1" {
		return
	}
	t.Log(strings.Repeat("large-output-", 20_000))
}

func BenchmarkSum(b *testing.B) {
	for range b.N {
		_ = Sum(2, 3)
	}
}

func BenchmarkZeroMetric(b *testing.B) {
	b.ReportMetric(0, "ns/op")
	for range b.N {
		_ = Sum(2, 3)
	}
}
