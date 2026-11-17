package romsource

import "testing"

const lolRomsContract = "../../../contract/openapi/lolroms.yaml"

func TestLolromsContractIsValidOpenAPI(t *testing.T) {
	doc := loadProviderContract(t, lolRomsContract)
	if doc.OpenAPI != "3.0.3" {
		t.Errorf("openapi = %q, want 3.0.3", doc.OpenAPI)
	}
	item := doc.Paths.Find("/")
	if item == nil || item.Get == nil {
		t.Fatal("path / has no GET operation")
	}
	param := item.Get.Parameters.GetByInAndName("query", "q")
	if param == nil || !param.Required {
		t.Fatalf("query parameter q = %+v, want required", param)
	}
	response := item.Get.Responses.Status(200)
	if response == nil || response.Value == nil {
		t.Fatal("path / has no 200 response")
	}
	media := response.Value.Content.Get("text/html")
	if media == nil || media.Schema == nil || !media.Schema.Value.Type.Is("string") {
		t.Fatalf("200 text/html media = %+v, want a string schema", media)
	}
}
