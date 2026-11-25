# ready-downloads
Objective: every download lands ready to play on Knulli first and on the Nix-installed emulators (t14, macm5), whatever the platform.
Branch: feat/ready-downloads. Delivery: ask-on-risk. Runner: `go test` via `nix shell nixpkgs#go_1_25`.

## Specs
- S1: Goal verbatim: "creo que el sistema debe dejar la descarga lista para cualquier posiblidad de platforma prefierencia knulli y emuladores de nixos".
- S2: A game that comes in several discs ("Driver 2 (USA) (Disc 1)", "(Disc 2)", also "(Disk N)", "(CD N)", "(Disc N of M)") gets one `.m3u` playlist named after the title without the disc tag (`Driver 2 (USA).m3u`) in the platform folder of the library. It lists every installed disc of that exact title (same region and revision) in disc order, with paths relative to the playlist, so RetroArch cores, Knulli and standalone emulators can swap discs from their menu. It exists only while two or more discs are installed and is rewritten when another disc installs.
- S3: Starting the download of one disc also starts the other discs of the same title and platform that the sources list, so the set is complete without the user hunting each disc. A sibling that cannot be found or started never fails the requested download.
- S4: The playlist never moves or renames an installed disc, so libraries already on disk keep working; a single-disc game gets no playlist.
- S5: The library folder of every platform is the system name Knulli scans (`gamecube`, `3ds`, `dreamcast`, plus the existing `megadrive`), taken from Knulli's `es_systems.yml`; `ngc`, `n3ds` and `dc` held no files, `arcade` (138 files) is untouched. Disc platforms (`psx`, `ps2`, `saturn`, `dc`) accept `.m3u`, as Knulli does.
- S6: The `heavy` badge carries a tooltip with the Knulli advice verified on the device for Conker: core `mupen64plus-next`, keep `.z64`, map the D-pad to the analog stick.

## Tasks
- [x] T1 (S2): domain `DiscOf` recognises the disc tag. inline; `naming.go`, 9 table cases
- [x] T2 (S2, S4): library writes and refreshes the playlist after a disc installs. inline; `romlibrary/playlist.go`, 5 unit tests plus a two-zip `Install` test
- [x] T3 (S3): `StartDownload` queues the sibling discs from the search sources and the platform listing, in the background. inline; 3 tests (search source, listing source, single disc)
- [x] T4: README, full checks. inline
- [x] T5 (S5, S6): Knulli folder names and `.m3u` on disc platforms; heavy-title tooltip. inline
- [ ] T6: release v0.1.10 and NixOS pin (docs/rom-destinations.md folder names): waits for the user's authorization

## Log
- L1: "creo que el sistema debe dejar la descarga lista para cualquier posiblidad de platforma prefierencia knulli y emuladores de nixos" (user goal, 2026-11-25), after asking how a two-disc game such as Driver 2 works on Knulli and other emulators.
- L2: Live evidence with the branch binary and `ROM_SOURCES=lolroms,archive.org`: a POST for `Driver 2 (USA) (Disc 1)` queued Disc 2 on its own (job 2); both finished (`psx/Driver 2 (USA) (Disc 1)` 618 MB bin, `psx/Driver 2 (USA) (Disc 2)` 575 MB bin) and `psx/Driver 2 (USA).m3u` held `Driver 2 (USA) (Disc 1)/Driver 2 (USA) (Disc 1).cue` and the Disc 2 line; every cue points to an existing bin. `go vet`, `go test -race`, `make lint` (0 issues) and `make check-providers` pass. Not tested: loading the `.m3u` on the Knulli device and swapping discs there. Risk: none.
- L3: Knulli device unreachable (`No route to host`, ping 100 percent loss), so `es_systems.cfg` could not be read live. Used the distribution source instead: `knulli-cfw/distribution` at `ab3a680`, `package/batocera/emulationstation/batocera-es-system/es_systems.yml`. `.m3u` is accepted for psx, ps2, saturn, dreamcast, gamecube, wii and segacd; not for psp or 3ds. Folder keys: `gamecube`, `3ds`, `dreamcast`, `lynx` (not `atarilynx`), `ngage`, `mame`/`fbneo`/`neogeo` (no `arcade`). Extension gaps left as they are, none blocks the common case: Knulli `nds` does not list `.dsi`, `3ds` lacks `.cia`/`.cci`, `psvita` wants `zip`/`psvita` not `.vpkg`, `ngage` wants `n-gage`/`sisx`/`sis` not `.ngage`/`.vpl`, `snes` lacks `.smd`. Changes: three `Dir` values, `.m3u` on four platforms, the heavy tooltip. `go vet`, `go test -race`, `make lint` (0 issues), `make check-providers` pass. Not tested: any emulator on t14 or macm5, the live Knulli. Risk: item 3 (the install folder of `ngc`, `n3ds` and `dc` changes; no file existed in them, and slugs and URLs are unchanged).

