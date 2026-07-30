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

package tag

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

type compliantResource struct {
	HTTPServerURL string `json:"http_server_url,omitempty" xml:"http_server_url,omitempty" yaml:"http_server_url,omitempty"`
	SHA256        string `json:"sha256,omitempty" xml:"sha256,omitempty" yaml:"sha256,omitempty"`
	private       string
}

type requiredResource struct {
	Name string `json:"name" xml:"name" yaml:"name"`
}

type missingResource struct {
	DisplayName string
}

type mismatchedResource struct {
	DisplayName string `json:"displayName,omitempty" xml:"display_name,omitempty" yaml:"display_name,omitempty"`
}

type nestedResource struct {
	Items []string `json:"items,omitempty" xml:"items>item,omitempty" yaml:"items,omitempty"`
}

type excludedResource struct {
	Secret string `json:"-" xml:"-" yaml:"-"`
}

type partiallyExcludedResource struct {
	Secret string `json:"-"`
}

type jsonResource struct {
	DurationNS int64  `json:"duration_ns,omitempty"`
	Required   string `json:"required"`
}

type presentMismatchResource struct {
	Name string `json:"wrong" xml:"wrong" yaml:"wrong"`
}

func TestGetTagCompliance(t *testing.T) {
	t.Run("default portable schema", func(t *testing.T) {
		messages, err := GetTagCompliance(&compliantResource{}, nil)
		if err != nil {
			t.Fatalf("GetTagCompliance() error = %v; messages = %v", err, messages)
		}
		if messages != nil {
			t.Fatalf("GetTagCompliance() messages = %v, want nil", messages)
		}
	})

	t.Run("typed nil pointer", func(t *testing.T) {
		var resource *compliantResource
		messages, err := GetTagCompliance(resource, nil)
		if err != nil {
			t.Fatalf("GetTagCompliance() error = %v; messages = %v", err, messages)
		}
	})

	t.Run("required fields omit omitempty", func(t *testing.T) {
		options := &Options{DisableTagOnEmpty: true}
		messages, err := GetTagCompliance(&requiredResource{}, options)
		if err != nil {
			t.Fatalf("GetTagCompliance() error = %v; messages = %v", err, messages)
		}

		messages, err = GetTagCompliance(&requiredResource{}, nil)
		if !errors.Is(err, ErrNonCompliant) {
			t.Fatalf("GetTagCompliance() error = %v, want ErrNonCompliant", err)
		}
		if len(messages) != 3 {
			t.Fatalf("GetTagCompliance() messages = %v, want 3 mismatches", messages)
		}
	})

	t.Run("missing tags have ordered diagnostics and suggestion", func(t *testing.T) {
		messages, err := GetTagCompliance(&missingResource{}, nil)
		if !errors.Is(err, ErrNonCompliant) {
			t.Fatalf("GetTagCompliance() error = %v, want ErrNonCompliant", err)
		}
		want := []string{
			`tag "json" not found in github.com/greenpau/tested/internal/tag.missingResource.DisplayName (string)`,
			`tag "xml" not found in github.com/greenpau/tested/internal/tag.missingResource.DisplayName (string)`,
			`tag "yaml" not found in github.com/greenpau/tested/internal/tag.missingResource.DisplayName (string)`,
			"suggested struct changes to github.com/greenpau/tested/internal/tag.missingResource:\n" +
				"DisplayName string `json:\"display_name,omitempty\" xml:\"display_name,omitempty\" yaml:\"display_name,omitempty\"`",
		}
		if !reflect.DeepEqual(messages, want) {
			t.Fatalf("GetTagCompliance() messages = %#v, want %#v", messages, want)
		}
	})

	t.Run("mismatched name is rejected", func(t *testing.T) {
		messages, err := GetTagCompliance(&mismatchedResource{}, nil)
		if !errors.Is(err, ErrNonCompliant) {
			t.Fatalf("GetTagCompliance() error = %v, want ErrNonCompliant", err)
		}
		if len(messages) != 1 || !strings.Contains(messages[0], `"displayName,omitempty"`) {
			t.Fatalf("GetTagCompliance() messages = %v, want JSON mismatch", messages)
		}
	})

	t.Run("exact field tag override", func(t *testing.T) {
		options := &Options{
			FieldTagOverrides: map[string]map[string]string{
				"Items": {
					"xml": "items>item,omitempty",
				},
			},
		}
		messages, err := GetTagCompliance(&nestedResource{}, options)
		if err != nil {
			t.Fatalf("GetTagCompliance() error = %v; messages = %v", err, messages)
		}

		options.FieldTagOverrides["Items"]["xml"] = "items>entry,omitempty"
		messages, err = GetTagCompliance(&nestedResource{}, options)
		if !errors.Is(err, ErrNonCompliant) {
			t.Fatalf("GetTagCompliance() error = %v, want ErrNonCompliant", err)
		}
		if len(messages) != 1 || !strings.Contains(messages[0], `"items>item,omitempty"`) {
			t.Fatalf("GetTagCompliance() messages = %v, want XML mismatch", messages)
		}
	})

	t.Run("legacy allowed field mismatch", func(t *testing.T) {
		options := &Options{
			AllowFieldMismatch: true,
			AllowedFields: map[string]interface{}{
				"items>item": true,
			},
		}
		messages, err := GetTagCompliance(&nestedResource{}, options)
		if err != nil {
			t.Fatalf("GetTagCompliance() error = %v; messages = %v", err, messages)
		}
	})

	t.Run("format exclusion is local to each tag", func(t *testing.T) {
		messages, err := GetTagCompliance(&excludedResource{}, nil)
		if err != nil {
			t.Fatalf("GetTagCompliance() error = %v; messages = %v", err, messages)
		}

		messages, err = GetTagCompliance(&partiallyExcludedResource{}, nil)
		if !errors.Is(err, ErrNonCompliant) {
			t.Fatalf("GetTagCompliance() error = %v, want ErrNonCompliant", err)
		}
		if len(messages) != 3 {
			t.Fatalf(
				"GetTagCompliance() messages = %v, want two missing tags and one suggestion",
				messages,
			)
		}
		if !strings.Contains(messages[0], `tag "xml"`) ||
			!strings.Contains(messages[1], `tag "yaml"`) {
			t.Fatalf("GetTagCompliance() messages = %v, want XML then YAML", messages)
		}
	})

	t.Run("selected format ignores options but not names", func(t *testing.T) {
		options := &Options{
			RequiredTags:      []string{"json"},
			DisableTagOnEmpty: true,
			IgnoreTagOptions:  true,
		}
		messages, err := GetTagCompliance(&jsonResource{}, options)
		if err != nil {
			t.Fatalf("GetTagCompliance() error = %v; messages = %v", err, messages)
		}

		messages, err = GetTagCompliance(&mismatchedResource{}, options)
		if !errors.Is(err, ErrNonCompliant) {
			t.Fatalf("GetTagCompliance() error = %v, want ErrNonCompliant", err)
		}
		if len(messages) != 1 {
			t.Fatalf("GetTagCompliance() messages = %v, want one mismatch", messages)
		}
	})

	t.Run("missing tags may be permitted", func(t *testing.T) {
		options := &Options{DisableTagPresent: true}
		messages, err := GetTagCompliance(&missingResource{}, options)
		if err != nil {
			t.Fatalf("GetTagCompliance() error = %v; messages = %v", err, messages)
		}

		messages, err = GetTagCompliance(&presentMismatchResource{}, options)
		if !errors.Is(err, ErrNonCompliant) {
			t.Fatalf("GetTagCompliance() error = %v, want ErrNonCompliant", err)
		}
		if len(messages) != 3 {
			t.Fatalf("GetTagCompliance() messages = %v, want 3 mismatches", messages)
		}
	})

	t.Run("present mismatches may be permitted", func(t *testing.T) {
		options := &Options{DisableTagMismatch: true}
		messages, err := GetTagCompliance(&presentMismatchResource{}, options)
		if err != nil {
			t.Fatalf("GetTagCompliance() error = %v; messages = %v", err, messages)
		}

		messages, err = GetTagCompliance(&missingResource{}, options)
		if !errors.Is(err, ErrNonCompliant) {
			t.Fatalf("GetTagCompliance() error = %v, want ErrNonCompliant", err)
		}
	})

	t.Run("disabled valid resource", func(t *testing.T) {
		messages, err := GetTagCompliance(
			&missingResource{},
			&Options{Disabled: true},
		)
		if err != nil {
			t.Fatalf("GetTagCompliance() error = %v; messages = %v", err, messages)
		}
	})
}

func TestGetTagComplianceRejectsUnsupportedResources(t *testing.T) {
	testCases := []struct {
		name     string
		resource interface{}
	}{
		{name: "nil", resource: nil},
		{name: "struct value", resource: compliantResource{}},
		{name: "pointer to scalar", resource: new(int)},
		{name: "pointer to pointer", resource: new(*compliantResource)},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			messages, err := GetTagCompliance(
				tc.resource,
				&Options{Disabled: true},
			)
			if !errors.Is(err, ErrUnsupportedResource) {
				t.Fatalf(
					"GetTagCompliance() error = %v, want ErrUnsupportedResource",
					err,
				)
			}
			if messages != nil {
				t.Fatalf("GetTagCompliance() messages = %v, want nil", messages)
			}
		})
	}
}

func TestGetTagComplianceRejectsInvalidOptions(t *testing.T) {
	testCases := []struct {
		name    string
		options *Options
	}{
		{
			name: "empty required tag",
			options: &Options{
				RequiredTags: []string{""},
			},
		},
		{
			name: "duplicate required tag",
			options: &Options{
				RequiredTags: []string{"json", "json"},
			},
		},
		{
			name: "raw literal delimiter in required tag",
			options: &Options{
				RequiredTags: []string{"json`invalid"},
			},
		},
		{
			name: "control rune in required tag",
			options: &Options{
				RequiredTags: []string{"json\u0085invalid"},
			},
		},
		{
			name: "unknown override field",
			options: &Options{
				FieldTagOverrides: map[string]map[string]string{
					"Unknown": {"json": "unknown"},
				},
			},
		},
		{
			name: "unselected override tag",
			options: &Options{
				RequiredTags: []string{"json"},
				FieldTagOverrides: map[string]map[string]string{
					"Name": {"xml": "name"},
				},
			},
		},
		{
			name: "empty override value",
			options: &Options{
				FieldTagOverrides: map[string]map[string]string{
					"Name": {"json": ""},
				},
			},
		},
		{
			name: "empty field overrides",
			options: &Options{
				FieldTagOverrides: map[string]map[string]string{
					"Name": {},
				},
			},
		},
		{
			name: "raw literal delimiter in override",
			options: &Options{
				FieldTagOverrides: map[string]map[string]string{
					"Name": {"json": "na`me"},
				},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			messages, err := GetTagCompliance(&requiredResource{}, tc.options)
			if !errors.Is(err, ErrInvalidOptions) {
				t.Fatalf(
					"GetTagCompliance() error = %v, want ErrInvalidOptions",
					err,
				)
			}
			if messages != nil {
				t.Fatalf("GetTagCompliance() messages = %v, want nil", messages)
			}
		})
	}
}

func TestConvertFieldToTag(t *testing.T) {
	testCases := []struct {
		field string
		want  string
	}{
		{field: "SHA256", want: "sha256"},
		{field: "DurationNS", want: "duration_ns"},
		{field: "TestOutputJSONL", want: "test_output_jsonl"},
		{field: "MediaType", want: "media_type"},
		{field: "RecommendedExitCode", want: "recommended_exit_code"},
		{field: "MD5Digest", want: "md5_digest"},
		{field: "OpenSSHKey", want: "openssh_key"},
		{field: "OAuth2Client", want: "oauth2_client"},
		{field: "CallbackURLs", want: "callback_urls"},
		{field: "URLsEnabled", want: "urls_enabled"},
		{field: "UserIDs", want: "user_ids"},
		{field: "APIURLs", want: "api_urls"},
		{field: "IPv4Address", want: "ipv4_address"},
		{field: "IPv6Address", want: "ipv6_address"},
		{field: "JWTs", want: "jwts"},
		{field: "PINs", want: "pins"},
		{field: "OIDCs", want: "oidcs"},
		{field: "BlurLs", want: "blur_ls"},
	}
	for _, tc := range testCases {
		t.Run(tc.field, func(t *testing.T) {
			if got := convertFieldToTag(tc.field); got != tc.want {
				t.Fatalf("convertFieldToTag(%q) = %q, want %q", tc.field, got, tc.want)
			}
		})
	}
}

func TestMakeTags(t *testing.T) {
	options := Options{
		FieldTagOverrides: map[string]map[string]string{
			"Items": {
				"xml": "items>item,omitempty",
			},
		},
	}
	got := makeTags("Items", []string{"json", "xml", "yaml"}, options)
	want := "`json:\"items,omitempty\" xml:\"items>item,omitempty\" yaml:\"items,omitempty\"`"
	if got != want {
		t.Fatalf("makeTags() = %q, want %q", got, want)
	}

	options.FieldTagOverrides["Items"]["xml"] = `items"entry`
	got = makeTags("Items", []string{"json", "xml", "yaml"}, options)
	tagLiteral := strings.TrimSuffix(strings.TrimPrefix(got, "`"), "`")
	xmlValue, present := reflect.StructTag(tagLiteral).Lookup("xml")
	if !present || xmlValue != `items"entry` {
		t.Fatalf(
			"makeTags() = %q, XML value = %q, present = %t",
			got,
			xmlValue,
			present,
		)
	}
}
