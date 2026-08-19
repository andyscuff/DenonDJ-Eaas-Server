# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

DenonDJ EAAS Server (module `github.com/andyscuff/denondj-eaas-server`) — a self-hosted
music library server that speaks the Denon StageLinQ/EAAS protocol, so Engine OS hardware
(Prime 4+, SC6000, etc.) can browse and stream a music library wirelessly without Engine DJ
Desktop running on a PC. It depends on `github.com/icedream/go-stagelinq` (an external
module, not vendored in this repo) for the EAAS protocol/proto types and beacon discovery.

All application code lives in `cmd/storage/` and compiles to a single `storage` binary
(package `main`, no internal packages). Despite the repo directory being named
`go-stagelinq`, this is the EAAS server, not the upstream library.

## Commands

```bash
go build ./cmd/storage        # build the binary (outputs ./storage per .gitignore convention)
go vet ./...                  # static checks
go run ./cmd/storage --music-dir /path/to/music
```

There is no test suite in this repo currently.

Runtime flags (see `cmd/storage/config.go`, `cmd/storage/main.go`):
- `--music-dir` (default `/srv/music`)
- `--navidrome-db` (default `/srv/navidrome/data/navidrome.db`, optional)
- `--host-ip` (auto-detected via UDP dial to 8.8.8.8 if unset)

Deployed as the `cubi-music` systemd service on this machine (`systemd/cubi-music.service`,
installed at `/etc/systemd/system/cubi-music.service`); restart with
`sudo systemctl restart cubi-music`, trigger an immediate rescan with
`sudo systemctl kill -s HUP cubi-music`. See `~/CLAUDE.md` for the sudo/hand-off convention
on this machine — Claude does not run `sudo` itself.

## Architecture

Single in-memory library rebuilt wholesale on every (re)scan, guarded by one `sync.RWMutex`
(`libraryMu` in `cmd/storage/library.go`). Reads (gRPC/HTTP handlers) take `RLock`; a rescan
builds a fresh set of `newAllTracks`/`newPlaylistMap` off to the side and swaps the globals
in under a brief `Lock`, so a rescan never blocks readers for long and never serves a
half-built tree. Rescans are triggered hourly (`rescanInterval` in `main.go`) or on `SIGHUP`.

**Library scan** (`loadLibrary` in `library.go`): walks `--music-dir` expecting
`Genre/Artist/Album/Track.ext` or `Genre/Artist/Track.ext` (both 2- and 3-level structures
are detected per-artist-folder by checking for audio files directly inside it). Builds a
`PlaylistNode` tree (Genre → Artist → Album) plus a flat `allTracks`/`trackMap` keyed by a
monotonically assigned int ID. Track tag metadata (title/artist/album/genre/year/artwork) is
read per-file via `github.com/dhowden/tag`; embedded picture bytes become `Track.Artwork` —
there is no artwork URL field in the `ListTrack` proto, so artwork must always be inlined as
bytes (`PreviewArtwork`), never referenced by URL, when populating search/browse results.
Playlist/track/genre/artist IDs are NFC-normalized (`golang.org/x/text/unicode/norm`) on
both write (scan) and read (`GetTracks` playlist lookup) since folder names on disk and IDs
round-tripped from the Engine OS client can differ in Unicode normalization form.

**Navidrome integration** (`navidrome.go`): read-only SQLite query (`?mode=ro`) against an
external Navidrome database, matching `media_file.path` (joined via `playlist_tracks`) back
to tracks already loaded from the filesystize scan by absolute path. Rebuilt on every
`loadLibrary` call and appended as a synthetic "My Playlists" `PlaylistNode`. Only playlists
with no `rules` (i.e. not Navidrome smart playlists) are read directly this way; NSP smart
playlist files (e.g. `.navidrome/Starred Tracks.nsp`) are Navidrome's own feature, not
something this server parses.

**gRPC layer** (`engine_library_service_server.go`, `network_trust_service_server.go`):
implements the `enginelibrary.EngineLibraryServiceServer` and
`networktrust.NetworkTrustServiceServer` interfaces from the upstream `go-stagelinq/eaas`
proto packages. `NetworkTrustServiceServer.CreateTrust` unconditionally grants every trust
request — there's no device allowlist. `GetTracks`/`SearchTracks` both respect
`req.GetPageSize()` (defaulting/capping as needed) rather than returning unbounded results —
keep that pattern when touching these handlers, since Engine OS clients paginate.

**Track URLs** (`trackURL` in `library.go`, consumed by `handleDownload` in `http.go`):
Engine OS devices require Windows-style paths wrapped as `<C:\...>`, not plain HTTP URLs —
`trackURL` maps a Linux path to that form, and `handleDownload` reverses the mapping
(strip `<>`, strip `C:`, backslash→slash) to serve the real file from disk.

**HTTP layer** (`http.go`, `gorilla/mux`): three routes — `/download/{path}` (serves audio
files, path format above), `/artwork/{id}` (serves embedded artwork bytes by track ID,
sniffs PNG vs JPEG by magic bytes), `/ping`. Served on `eaas.DefaultEAASHTTPPort`
(port 50020); gRPC on `eaas.DefaultEAASGRPCPort` (port 50010); beacon UDP discovery on
port 11224 — all three must be open on the firewall for hardware to find and use the server.

**Beacon/discovery**: `main.go` starts an EAAS beacon (`eaas.StartBeaconWithConfiguration`)
advertising a fixed 16-byte `cubiToken`. `grpc.MaxSendMsgSize` is raised to 32MiB on the
server (needed headroom for embedded artwork in track/search responses).

## Gotchas

- No package boundaries inside `cmd/storage` — everything is `package main`; there's no
  library surface to import elsewhere in this repo.
- `loadLibrary` fully rebuilds and swaps all four globals (`allTracks`, `allPlaylists`,
  `trackMap`, `playlistMap`) — don't mutate them incrementally, and don't hold `libraryMu`
  across I/O (file reads, SQLite queries) beyond what's already structured that way.
- IDs built from folder names (`"genre-"+name`, `"artist-"+genre+"-"+artist`,
  `"album-"+genre+"-"+artist+"-"+album`) are plain string concatenation with no separator
  escaping — a folder name containing `-` in the right place can theoretically collide with
  a different path's ID. Known limitation, not yet fixed.
