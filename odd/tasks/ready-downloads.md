# ready-downloads
Objective: every download lands ready to play on Knulli first and on the Nix-installed emulators (t14, macm5), whatever the platform.
Branch: feat/ready-downloads. Delivery: ask-on-risk. Runner: `go test` via `nix shell nixpkgs#go_1_25`.

## Specs
- S1: Goal verbatim: "creo que el sistema debe dejar la descarga lista para cualquier posiblidad de platforma prefierencia knulli y emuladores de nixos".
- S2: A game that comes in several discs ("Driver 2 (USA) (Disc 1)", "(Disc 2)", also "(Disk N)", "(CD N)", "(Disc N of M)") gets one `.m3u` playlist named after the title without the disc tag (`Driver 2 (USA).m3u`) in the platform folder of the library. It lists every installed disc of that exact title (same region and revision) in disc order, with paths relative to the playlist, so RetroArch cores, Knulli and standalone emulators can swap discs from their menu. It exists only while two or more discs are installed and is rewritten when another disc installs.
- S3: Starting the download of one disc also starts the other discs of the same title and platform that the sources list, so the set is complete without the user hunting each disc. A sibling that cannot be found or started never fails the requested download.
- S4: The playlist never moves or renames an installed disc, so libraries already on disk keep working; a single-disc game gets no playlist.

## Tasks
- [x] T1 (S2): domain `DiscOf` recognises the disc tag. inline; `naming.go`, 9 table cases
- [x] T2 (S2, S4): library writes and refreshes the playlist after a disc installs. inline; `romlibrary/playlist.go`, 5 unit tests plus a two-zip `Install` test
- [x] T3 (S3): `StartDownload` queues the sibling discs from the search sources and the platform listing, in the background. inline; 3 tests (search source, listing source, single disc)
- [~] T4: README done, full checks done; release and pin pending the user's authorization. inline

## Log
- L1: "creo que el sistema debe dejar la descarga lista para cualquier posiblidad de platforma prefierencia knulli y emuladores de nixos" (user goal, 2026-11-25), after asking how a two-disc game such as Driver 2 works on Knulli and other emulators.
- L2: Live evidence with the branch binary and `ROM_SOURCES=lolroms,archive.org`: a POST for `Driver 2 (USA) (Disc 1)` queued Disc 2 on its own (job 2); both finished (`psx/Driver 2 (USA) (Disc 1)` 618 MB bin, `psx/Driver 2 (USA) (Disc 2)` 575 MB bin) and `psx/Driver 2 (USA).m3u` held `Driver 2 (USA) (Disc 1)/Driver 2 (USA) (Disc 1).cue` and the Disc 2 line; every cue points to an existing bin. `go vet`, `go test -race`, `make lint` (0 issues) and `make check-providers` pass. Not tested: loading the `.m3u` on the Knulli device and swapping discs there. Risk: none.
