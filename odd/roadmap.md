# ROMHarbor roadmap
Parent map: direction, capabilities, dependencies and delivery boundaries. Each feature has one owner document for tasks and proof; this file only links them.

## Product
A self-hosted community application to browse ROM catalogs, find games, download them safely into a per-platform library, and hand that library to RomM. NixOS consumes it as a pinned flake; releases are versioned images on GHCR.

## Deliveries
- D1 catalog-extraction: complete (v0.1.0). Owner: `odd/tasks/catalog-extraction.md`.
- D2 community-distribution: complete (v0.1.0). Owner: `odd/tasks/community-distribution.md`.
- D3 provider-contracts: complete (v0.1.1). Owner: `odd/tasks/provider-contracts.md`.
- D4 durable-jobs: complete (v0.1.3). Owner: `odd/tasks/durable-jobs.md`.
- D5 adapter-audit: selected. Owner: `odd/tasks/adapter-audit.md`. Goal: every adapter defect is inventoried once, classified, and closed against one ledger instead of being found one by one.

## Named, not expanded
- Behavior-named CQRS surface (request download, get download status, list downloads, search/browse games). Depends on the D5 decisions about ports.
- Whether platform browsing and text search both stay.
- Acceptance gaps carried from earlier deliveries: live provider acceptance, fresh acquisition to library to RomM, gameplay and save-state reload on macm5, Powkiddy transfer.
- Download completion notice (`JobNotifier`): a new feature outside the D5 ledger; the user has not chosen the channel.
- Retail PlayStation sources (diagnosed 2026-11-24, nothing changed): `dlarchive` lists nothing (`File listing is not public`, an explicit upstream decision, no project fix); `lolroms` is slow (7-21 s first byte, the one-minute client timeout is sometimes exceeded) and its console map `psx -> "Sony PlayStation"` never matches the site's `PlayStation` folder (`lolroms.go:60`), so every PS1 hit is dropped; archive.org `psx` only lists the `non-redump_sony_playstation` set (betas and prototypes) while public Redump sets exist (`redump-org-sony-playstation-pal`, `Redump.orgSonyPlayStation-NTSC-U-*`, one zip per disc). Candidate first step: extend the archive.org `psx` set list (`archiveorg.go:87`), then fix the lolroms console map. Multi-disc titles list each disc apart until `.m3u` (finding N-03, K6) is handled. The user set PS1 aside for now.

## Dependencies
D5 rules (checks) precede D5 corrections. The CQRS surface follows D5.
