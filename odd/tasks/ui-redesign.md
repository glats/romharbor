# ui-redesign
Objective: redesign the ROMHarbor web interface on the same base (Go templates, htmx, no SPA) so it is usable now, shows downloads, filters, and makes Knulli compatibility visible and enforced.
Branch: feat/ui-redesign (from main at the K4 close). Delivery: ask-on-risk. Runner: `TMPDIR=/tmp/opencode GOTOOLCHAIN=local nix shell nixpkgs#go_1_25 -c sh -c '...'`.

## Specs
S1. Same base. User answer, verbatim label: "Rediseño visual, misma base" — server-rendered Go templates and htmx; no SPA, no new frontend build.
S2. Scope, user answer verbatim: "Estilo general, Ver descargas, Buscar y filtrar, Compatibilidad a la vista, siempre se tiene que forzar a la compatibilidad con canoris pero si no bueno le ponemos la etiqueta" (assumption: "canoris" is Knulli). Four areas: general style (colors, typography, cards, light and dark theme: assumption from the offered option), visible downloads panel (in progress, done, failed, with progress), search and filter (global search across platforms, filters, order), compatibility shown on each game.
S3. Knulli compatibility rule (assumption from S2, to be confirmed by the user): the catalog prefers and forces the Knulli-compatible variant by default; when a game has no compatible variant it is still listed with a label that says why (translation, beta, PAL, prototype, heavy title). The rules come from the compatibility research in `odd/roadmap.md` (clean No-Intro/Redump over translations, hacks and betas; `.z64` for N64; NTSC/US over PAL; CHD over cue/bin).
S4. Out of scope until the user asks: new cores in the NixOS repository, `.m3u` handling (K6/N-03), publishing, tag, pin.

## Tasks
(to be filled after the exploration map)

## Log
- L1: "sigamos adelante quiero empezar a usarlo pero ya y adems vamos a cambiar la interfaces visuales"
- L2: "Rediseño visual, misma base"
- L3: "Estilo general, Ver descargas, Buscar y filtrar, Compatibilidad a la vista, siempre se tiene que forzar a la compatibilidad con canoris pero si no bueno le ponemos la etiqueta"
- L4: The running v0.1.5 on `rog` already serves `rom.glats.org`; the user's earlier statement stays: "todos los roms la idea es que sean lo maximo compatible con knulli y el sistema tiene que proveer eso".
