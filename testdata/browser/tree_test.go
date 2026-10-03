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

package browser

import "testing"

func TestTree(t *testing.T) {
	t.Run("group", func(t *testing.T) {
		t.Run("pass", func(t *testing.T) {
			t.Parallel()
			t.Log("passing output stays available")
		})
		t.Run("fail", func(t *testing.T) {
			t.Parallel()
			t.Fatal("unique-leaf-failure <script>window.injected = true</script>")
		})
		t.Run("skip", func(t *testing.T) { t.Skip("intentional skip") })
	})
}

var unevenRuns int

func TestUneven(t *testing.T) {
	unevenRuns++
	if unevenRuns == 2 {
		t.Run("only-second-run", func(t *testing.T) { t.Log("second parent, first child") })
	}
}

func TestRedacted(t *testing.T) {
	for _, name := range []string{"secret-one", "secret-two"} {
		t.Run(name, func(t *testing.T) {
			t.Run("<img_src=x_onerror=alert(1)>", func(t *testing.T) { t.Log("inert text") })
		})
	}
}

func TestDeep(t *testing.T) {
	var descend func(*testing.T, int)
	descend = func(t *testing.T, depth int) {
		if depth == 0 {
			t.Log("deep leaf output")
			return
		}
		t.Run("level", func(t *testing.T) { descend(t, depth-1) })
	}
	descend(t, 12)
}
