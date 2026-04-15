# DenonDJ EAAS Server

A self-hosted music library server for Denon DJ Engine OS devices via the EAAS/StageLinQ protocol. Streams your music library wirelessly to Engine OS hardware (Prime 4+, SC6000 etc) without needing Engine DJ Desktop software running on a PC.

Built on top of [go-stagelinq](https://github.com/icedream/go-stagelinq) by Carl Kittelberger, which implements the Denon StageLinQ/EAAS protocol.

## Features

- Serves FLAC, MP3, WAV, AIFF and M4A files
- Genre → Artist → Album playlist hierarchy derived from folder structure
- Handles both 2-level (Genre/Artist/Track) and 3-level (Genre/Artist/Album/Track) structures
- Album artwork served from embedded file tags
- Full text search across title, artist, album, genre and filename
- Navidrome playlist integration — playlists created in Navidrome appear on your DJ hardware
- Auto-detects host IP address for artwork serving
- Hourly auto-rescan + instant rescan via SIGHUP
- Runs as a systemd service on Linux

## Requirements

- Linux (tested on Ubuntu 24.04)
- Go 1.19 or newer
- Music library organised as `Genre/Artist/Album/Track.ext` or `Genre/Artist/Track.ext`

## Installation

```bash
git clone https://github.com/andyscuff/DenonDJ-Eaas-Server.git
cd DenonDJ-Eaas-Server
go build ./cmd/storage
```

## Usage

```bash
./storage --music-dir /path/to/music
```

### Options

| Flag | Default | Description |
|------|---------|-------------|
| `--music-dir` | `/srv/music` | Path to music library root |
| `--navidrome-db` | `/srv/navidrome/data/navidrome.db` | Path to Navidrome SQLite database (optional) |
| `--host-ip` | auto-detected | Host IP address for artwork URLs |

### Example

```bash
./storage \
  --music-dir /home/andy/Music \
  --navidrome-db /opt/navidrome/navidrome.db \
  --host-ip 192.168.1.100
```

## Running as a systemd service

Copy the example service file and edit it for your setup:

```bash
sudo cp systemd/cubi-music.service /etc/systemd/system/
sudo nano /etc/systemd/system/cubi-music.service
sudo systemctl daemon-reload
sudo systemctl enable cubi-music
sudo systemctl start cubi-music
```

To trigger an immediate rescan after adding new music:

```bash
sudo systemctl kill -s HUP cubi-music
```

## Navidrome Integration

If you run [Navidrome](https://navidrome.org) as your music server, this tool will automatically read your Navidrome playlists and present them on your Denon hardware under a "My Playlists" section.

Playlists are read directly from Navidrome's SQLite database — no configuration needed beyond pointing `--navidrome-db` at the database file.

### Smart Playlists / Starred Tracks

You can create a smart playlist in Navidrome that automatically populates with tracks you've starred in Symfonium (or any Subsonic client). Create a file at `<music-dir>/.navidrome/Starred Tracks.nsp`:

```json
{
  "all": [{ "is": { "loved": true } }],
  "sort": "dateLoved",
  "order": "desc"
}
```

## Music Library Structure

The server expects music organised in folders like this:
Music/
├── Jazz/
│   ├── Miles Davis/
│   │   ├── Kind of Blue/
│   │   │   ├── So What.flac
│   │   │   └── All Blues.flac
├── Electronic/
│   ├── Aphex Twin/
│   │   ├── Selected Ambient Works.flac

Both 3-level (Genre/Artist/Album/Track) and 2-level (Genre/Artist/Track) structures are supported.

## Firewall

The following ports need to be open on your server:

| Port | Protocol | Purpose |
|------|----------|---------|
| 11224 | UDP | EAAS device discovery |
| 50010 | TCP | EAAS gRPC |
| 50020 | TCP | HTTP (artwork + file serving) |

## Credits

Built on top of [go-stagelinq](https://github.com/icedream/go-stagelinq) by [Carl Kittelberger (icedream)](https://github.com/icedream).

## License

MIT
