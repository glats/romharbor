# Provider contracts

ROMHarbor keeps consumer-maintained OpenAPI 3.0.3 contracts for the endpoints of
its three ROM providers (two JSON APIs and one HTML search page) and generates
their HTTP clients locally. The
contracts describe the wire shapes ROMHarbor relies on. Their reference is the
official Internet Archive read documentation at
https://archive.org/developers/md-read.html; for dlarchive, the contract is
based on observed behavior, and no upstream OpenAPI document was found in a
bounded search.

## Quick path

1. Edit the contract in `contract/openapi/` or its generator config in `config/oapi-codegen/`.
2. Regenerate and normalize the clients: `make generate-providers`.
3. Run the provider tests: `go test ./internal/adapter/romsource/... -run 'Test(ArchiveOrg|DlArchive|LolRoms|Provider)'`.

`make check-providers` is the intended read-only drift check for the generated
clients. It compares a fresh generation with the committed files.

## Details

| Contract | Endpoint | Generated package | Consumer |
|----------|----------|-------------------|----------|
| `contract/openapi/archiveorg.yaml` | `GET /metadata/{identifier}` | `internal/adapter/romsource/archiveorgapi` | `ArchiveOrg` source |
| `contract/openapi/dlarchive.yaml` | `GET /files_api.php` with `action=list` or `action=file-url` | `internal/adapter/romsource/dlarchiveapi` | `DlArchive` source |
| `contract/openapi/lolroms.yaml` | `GET /` with query `q`, answering `text/html` | `internal/adapter/romsource/lolromsapi` | `LolRoms` source (goquery parses the page) |

The DlArchive path is one GET operation. Its `action` parameter selects a list
page, a file URL, or a provider error payload (`ok` false), and the contract
expresses those variants with `anyOf`.

Generated `client.gen.go` files carry the `DO NOT EDIT` header. Change the
contract or the config instead. Adapters keep their mutable `HTTPClient` and
`BaseURL` fields and build the generated client from them on each request. Fixed request headers are set through `romfetch.Headers`, and no adapter outside the generated packages and `romfetch` imports `net/http` (a lint rule enforces it).
Provider-to-domain mapping stays handwritten in `archiveorg.go` and `dlarchive.go`.

## Consumer-owned semantics

The contracts model only the fields ROMHarbor reads, plus the documented
variants that the adapter must recognize. Fields that the baseline never decoded
(for example the Archive `error` and `metadata` properties) are not modeled, so
unexpected types there are ignored.

Behavior kept by design: the Archive empty-array variant (an unknown identifier) is reported as an error; a DlArchive error payload is reported as a missing download URL by `FileURL`; the list action treats an `ok` false page as an empty index. A base URL with a trailing slash is normalized, so requests never carry a double slash. Decode and status errors carry the wording of the generated client, wrapped with the provider and operation; response bodies are read through an explicit size bound and an oversized body is an error.
