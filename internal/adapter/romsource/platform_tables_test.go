package romsource

import (
	"testing"

	"app.ddd/internal/domain/rom"
)

func TestEverySourcePlatformIsADomainPlatform(t *testing.T) {
	tables := map[string][]string{}
	for slug := range lolRomsConsoles {
		tables["lolroms"] = append(tables["lolroms"], slug)
	}
	for slug := range dlArchiveFolders {
		tables["dlarchive"] = append(tables["dlarchive"], slug)
	}
	for slug := range NewArchiveOrg(nil).Sets {
		tables["archive.org"] = append(tables["archive.org"], slug)
	}
	for source, slugs := range tables {
		for _, slug := range slugs {
			if _, ok := rom.PlatformBySlug(slug); !ok {
				t.Errorf("%s serves platform %q, which the domain does not know", source, slug)
			}
		}
	}
}
