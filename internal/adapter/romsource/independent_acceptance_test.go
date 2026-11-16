package romsource_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

func TestIndependentWireShapes(t *testing.T) {
	for _, tt := range []struct {
		contract, path, body string
		valid                bool
	}{
		{"archiveorg", "/metadata/{identifier}", `{"files":[{"name":"A.nes","size":17},{"name":"B.nes","size":"18"},{"name":"C.nes","size":null},{}]}`, true},
		{"archiveorg", "/metadata/{identifier}", `[]`, true},
		{"archiveorg", "/metadata/{identifier}", `{"files":[{"size":false}]}`, false},
		{"dlarchive", "/files_api.php", `{"items":[{"size":17}],"total":1}`, true},
		{"dlarchive", "/files_api.php", `{"items":[{"size":"17"}],"total":1}`, false},
		{"dlarchive", "/files_api.php", `{"ok":false,"error":"not found"}`, true},
		{"dlarchive", "/files_api.php", `{"ok":true,"url":"https://files.invalid/A.zip"}`, true},
	} {
		t.Run(tt.contract+tt.body, func(t *testing.T) {
			doc, err := openapi3.NewLoader().LoadFromFile("../../../contract/openapi/" + tt.contract + ".yaml")
			if err != nil {
				t.Fatal(err)
			}
			if err = doc.Validate(context.Background()); err != nil {
				t.Fatal(err)
			}
			var value any
			if err = json.Unmarshal([]byte(tt.body), &value); err != nil {
				t.Fatal(err)
			}
			schema := doc.Paths.Find(tt.path).Get.Responses.Status(200).Value.Content.Get("application/json").Schema.Value
			if err = schema.VisitJSON(value); (err == nil) != tt.valid {
				t.Fatalf("valid=%t: %v", tt.valid, err)
			}
		})
	}
}
