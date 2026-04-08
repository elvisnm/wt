package docker

import (
	"testing"

	"github.com/elvisnm/wt/internal/cmdutil"
)

func TestParseJsonLines_Single(t *testing.T) {
	raw := `{"Names":"myapp-feat","State":"running","Status":"Up 2 hours (healthy)"}`
	result := cmdutil.ParseJSONLines(raw)
	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}
	if result[0]["Names"] != "myapp-feat" {
		t.Errorf("expected Names='myapp-feat', got %q", result[0]["Names"])
	}
}

func TestParseJsonLines_Multiple(t *testing.T) {
	raw := `{"Names":"myapp-feat","State":"running"}
{"Names":"myapp-fix","State":"exited"}
{"Names":"myapp-dev","State":"running"}`
	result := cmdutil.ParseJSONLines(raw)
	if len(result) != 3 {
		t.Fatalf("expected 3 results, got %d", len(result))
	}

	expected_names := []string{"myapp-feat", "myapp-fix", "myapp-dev"}
	for i, name := range expected_names {
		got := result[i]["Names"]
		if got != name {
			t.Errorf("result[%d] Names: expected %q, got %q", i, name, got)
		}
	}
}

func TestParseJsonLines_EmptyInput(t *testing.T) {
	result := cmdutil.ParseJSONLines("")
	if len(result) != 0 {
		t.Errorf("expected 0 results for empty input, got %d", len(result))
	}
}

func TestParseJsonLines_BlankLines(t *testing.T) {
	raw := `
{"Names":"myapp-feat"}

{"Names":"myapp-fix"}

`
	result := cmdutil.ParseJSONLines(raw)
	if len(result) != 2 {
		t.Fatalf("expected 2 results (ignoring blank lines), got %d", len(result))
	}
}

func TestParseJsonLines_InvalidJson(t *testing.T) {
	raw := `{"Names":"myapp-feat"}
not valid json
{"Names":"myapp-fix"}`
	result := cmdutil.ParseJSONLines(raw)
	if len(result) != 2 {
		t.Fatalf("expected 2 results (skipping invalid), got %d", len(result))
	}
}

func TestParseJsonLines_NestedJson(t *testing.T) {
	raw := `{"name":"web","pm2_env":{"status":"online"},"monit":{"memory":104857600,"cpu":2.5}}`
	result := cmdutil.ParseJSONLines(raw)
	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}
	if result[0]["name"] != "web" {
		t.Errorf("expected name='web', got %q", result[0]["name"])
	}
	// nested fields should be preserved as map[string]interface{}
	if _, ok := result[0]["pm2_env"].(map[string]interface{}); !ok {
		t.Error("expected pm2_env to be a nested map")
	}
}

func TestGetStringField_SingleKey(t *testing.T) {
	data := map[string]interface{}{
		"Names": "myapp-feat",
		"State": "running",
	}
	got := cmdutil.GetStringField(data, "Names")
	if got != "myapp-feat" {
		t.Errorf("expected 'myapp-feat', got %q", got)
	}
}

func TestGetStringField_FallbackKey(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]interface{}
		keys     []string
		expected string
	}{
		{
			name:     "first key matches",
			data:     map[string]interface{}{"Names": "myapp-feat"},
			keys:     []string{"Names", "names"},
			expected: "myapp-feat",
		},
		{
			name:     "second key matches",
			data:     map[string]interface{}{"names": "myapp-feat"},
			keys:     []string{"Names", "names"},
			expected: "myapp-feat",
		},
		{
			name:     "no key matches",
			data:     map[string]interface{}{"other": "value"},
			keys:     []string{"Names", "names"},
			expected: "",
		},
		{
			name:     "non-string value ignored",
			data:     map[string]interface{}{"Names": 42},
			keys:     []string{"Names"},
			expected: "",
		},
		{
			name:     "empty map",
			data:     map[string]interface{}{},
			keys:     []string{"Names"},
			expected: "",
		},
		{
			name:     "no keys provided",
			data:     map[string]interface{}{"Names": "myapp"},
			keys:     []string{},
			expected: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := cmdutil.GetStringField(tc.data, tc.keys...)
			if got != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, got)
			}
		})
	}
}

func TestGetStringField_NilMap(t *testing.T) {
	// Passing nil data should not panic
	got := cmdutil.GetStringField(nil, "key")
	if got != "" {
		t.Errorf("expected empty string for nil map, got %q", got)
	}
}
