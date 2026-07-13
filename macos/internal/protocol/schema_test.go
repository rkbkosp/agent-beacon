package protocol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v5"
)

func protocolRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "protocol"))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func loadJSON(t *testing.T, path string) any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestProtocolExamplesMatchSchemas(t *testing.T) {
	root := protocolRoot(t)
	cases := []struct {
		schema  string
		example string
	}{
		{"notification-v2.schema.json", "agent-done.json"},
		{"notification-v2.schema.json", "agent-blocked.json"},
		{"notification-v2.schema.json", "quota-low.json"},
		{"notification-v2.schema.json", "umbrella-required.json"},
		{"snapshot-v2.schema.json", "snapshot.json"},
		{"ack-v2.schema.json", "ack.json"},
	}
	for _, testCase := range cases {
		t.Run(testCase.example, func(t *testing.T) {
			schema, err := jsonschema.Compile("file://" + filepath.Join(root, "schema", testCase.schema))
			if err != nil {
				t.Fatal(err)
			}
			if err := schema.Validate(loadJSON(t, filepath.Join(root, "examples", testCase.example))); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNotificationSchemaRejectsUnknownTheme(t *testing.T) {
	root := protocolRoot(t)
	schema, err := jsonschema.Compile("file://" + filepath.Join(root, "schema", "notification-v2.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	value := loadJSON(t, filepath.Join(root, "examples", "agent-done.json")).(map[string]any)
	value["payload"].(map[string]any)["theme"] = "purple"
	if err := schema.Validate(value); err == nil {
		t.Fatal("expected unknown notification theme to fail schema validation")
	}
}
