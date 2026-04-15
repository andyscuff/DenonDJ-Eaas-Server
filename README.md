# DenonDJ EAAS Server

A self-hosted music library server for Denon DJ Engine OS devices via the EAAS/StageLinQ protocol. Streams your music library wirelessly to Engine OS hardware (Prime 4+, SC6000 etc) without needing Engine DJ desktop software running.

## Features

- Serves FLAC, MP3, WAV, AIFF and M4A files
- Genre → Artist → Album playlist hierarchy derived from folder structure
- Album artwork served from embedded tags
- Full text search across title, artist, album, genre and filename
- Navidrome playlist integration — playlists created in Navidrome appear on your DJ hardware
- Hourly auto-rescan + instant rescan via SIGHUP
- Runs as a systemd service on Linux

## Requirements

- Linux (tested on Ubuntu 24.04)
- Go 1.19+
- Music library organised as `Genre/Artist/Album/Track.ext`

## Installation

```bash
git clone https://github.com/andyscuff/DenonDJ-Eaas-Server.git
cd DenonDJ-Eaas-Server
go build ./cmd/storage
```

## Configuration

Edit `cmd/storage/library.go` and set `musicRoot` to your music folder path.

If using Navidrome playlist integration, edit `cmd/storage/navidrome.go` and set `navidromeDB` to your Navidrome database path.

## Running

```bash
./storage
```

Or as a systemd service — see `systemd/cubi-music.service` for an example unit file.

## Rescanning

To trigger an immediate rescan after adding new music:

```bash
sudo systemctl kill -s HUP cubi-music
```

## Credits

Built on top of [go-stagelinq](https://github.com/icedream/go-stagelinq) by Carl Kittelberger (icedream), which implements the Denon StageLinQ/EAAS protocol.

## License

MIT
