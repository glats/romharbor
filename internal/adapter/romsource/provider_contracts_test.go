package romsource

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"app.ddd/internal/adapter/romsource/archiveorgapi"
	"app.ddd/internal/adapter/romsource/dlarchiveapi"
	"github.com/getkin/kin-openapi/openapi3"
)

const (
	archiveOrgContract = "../../../contract/openapi/archiveorg.yaml"
	dlArchiveContract  = "../../../contract/openapi/dlarchive.yaml"
)

func loadProviderContract(t *testing.T, path string) *openapi3.T {
	t.Helper()
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = false
	doc, err := loader.LoadFromFile(path)
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("validate %s: %v", path, err)
	}
	return doc
}

func providerResponseSchema(t *testing.T, doc *openapi3.T, path string) *openapi3.Schema {
	t.Helper()
	item := doc.Paths.Find(path)
	if item == nil || item.Get == nil {
		t.Fatalf("path %s has no GET operation", path)
	}
	response := item.Get.Responses.Status(200)
	if response == nil || response.Value == nil {
		t.Fatalf("path %s has no 200 response", path)
	}
	media := response.Value.Content.Get("application/json")
	if media == nil || media.Schema == nil {
		t.Fatalf("path %s has no application/json schema", path)
	}
	return media.Schema.Value
}

func readProviderFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "providers", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return raw
}

func TestProviderContractsAreValidOpenAPI(t *testing.T) {
	for _, path := range []string{archiveOrgContract, dlArchiveContract} {
		doc := loadProviderContract(t, path)
		if doc.OpenAPI != "3.0.3" {
			t.Errorf("%s: openapi = %q, want 3.0.3", path, doc.OpenAPI)
		}
	}
}

func TestProviderFixturesMatchContracts(t *testing.T) {
	cases := []struct {
		contract string
		endpoint string
		fixture  string
		valid    bool
	}{
		{archiveOrgContract, "/metadata/{identifier}", "archiveorg/metadata-number-sizes.json", true},
		{archiveOrgContract, "/metadata/{identifier}", "archiveorg/metadata-string-sizes.json", true},
		{archiveOrgContract, "/metadata/{identifier}", "archiveorg/metadata-missing-optional.json", true},
		{archiveOrgContract, "/metadata/{identifier}", "archiveorg/metadata-empty-array.json", true},
		{archiveOrgContract, "/metadata/{identifier}", "archiveorg/metadata-boolean-size.json", false},
		{dlArchiveContract, "/files_api.php", "dlarchive/files-list.json", true},
		{dlArchiveContract, "/files_api.php", "dlarchive/files-file-url.json", true},
		{dlArchiveContract, "/files_api.php", "dlarchive/files-file-url-failure.json", true},
		{dlArchiveContract, "/files_api.php", "dlarchive/files-string-size.json", false},
	}
	docs := map[string]*openapi3.T{
		archiveOrgContract: loadProviderContract(t, archiveOrgContract),
		dlArchiveContract:  loadProviderContract(t, dlArchiveContract),
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			schema := providerResponseSchema(t, docs[tc.contract], tc.endpoint)
			var value any
			if err := json.Unmarshal(readProviderFixture(t, tc.fixture), &value); err != nil {
				t.Fatalf("decode fixture: %v", err)
			}
			err := schema.VisitJSON(value)
			if tc.valid && err != nil {
				t.Fatalf("fixture rejected: %v", err)
			}
			if !tc.valid && err == nil {
				t.Fatal("fixture accepted, want rejected")
			}
		})
	}
}

func TestProviderFixturesDecodeIntoGeneratedModels(t *testing.T) {
	var meta archiveorgapi.ItemMetadata
	if err := json.Unmarshal(readProviderFixture(t, "archiveorg/metadata-string-sizes.json"), &meta); err != nil {
		t.Fatalf("decode archive metadata: %v", err)
	}
	wantSizes := []int64{40960, 20480, 0}
	if len(meta.Files) != len(wantSizes) {
		t.Fatalf("files = %d, want %d", len(meta.Files), len(wantSizes))
	}
	for i, file := range meta.Files {
		var size sizeValue
		if err := size.UnmarshalJSON(file.Size); err != nil {
			t.Fatalf("file %d size: %v", i, err)
		}
		if int64(size) != wantSizes[i] {
			t.Errorf("file %d size = %d, want %d", i, size, wantSizes[i])
		}
	}
	if meta.Files[0].Md5 == "" || meta.Files[0].Sha1 == "" {
		t.Errorf("checksums not decoded: %+v", meta.Files[0])
	}

	var sparse archiveorgapi.ItemMetadata
	if err := json.Unmarshal(readProviderFixture(t, "archiveorg/metadata-missing-optional.json"), &sparse); err != nil {
		t.Fatalf("decode sparse archive metadata: %v", err)
	}
	missing := sparse.Files[1]
	if missing.Name == "" || missing.Md5 != "" || missing.Sha1 != "" || missing.Format != "" || missing.Source != "" || missing.Size != nil {
		t.Errorf("missing optional fields decoded unexpectedly: %+v", missing)
	}

	var list dlarchiveapi.DlArchiveListResponse
	if err := json.Unmarshal(readProviderFixture(t, "dlarchive/files-list.json"), &list); err != nil {
		t.Fatalf("decode dlarchive list: %v", err)
	}
	if len(list.Items) != 1 || list.Total != 1 {
		t.Fatalf("list = %+v", list)
	}
	item := list.Items[0]
	if item.Id != "example-1" || item.Size != 40960 || item.DownloadUrl == "" || item.Folder == "" {
		t.Errorf("item = %+v", item)
	}

	var answer dlarchiveapi.DlArchiveFileURLResponse
	if err := json.Unmarshal(readProviderFixture(t, "dlarchive/files-file-url.json"), &answer); err != nil {
		t.Fatalf("decode dlarchive file url: %v", err)
	}
	if !answer.Ok || answer.Url == "" {
		t.Errorf("answer = %+v", answer)
	}
}
