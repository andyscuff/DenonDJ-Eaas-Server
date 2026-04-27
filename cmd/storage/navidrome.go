package main

import (
	"database/sql"
	"log"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

func loadNavidromePlaylists() []*PlaylistNode {
	dbPath := getNavidromeDB()
	db, err := sql.Open("sqlite3", dbPath+"?mode=ro")
	if err != nil {
		log.Printf("Navidrome: failed to open DB: %v", err)
		return nil
	}
	defer db.Close()

	rows, err := db.Query(`SELECT id, name FROM playlist WHERE rules IS NULL OR rules = '' ORDER BY name`)
	if err != nil {
		log.Printf("Navidrome: failed to query playlists: %v", err)
		return nil
	}
	defer rows.Close()

	type ndPlaylist struct {
		id   string
		name string
	}
	var playlists []ndPlaylist
	for rows.Next() {
		var p ndPlaylist
		if err := rows.Scan(&p.id, &p.name); err != nil {
			continue
		}
		playlists = append(playlists, p)
	}

	if len(playlists) == 0 {
		return nil
	}

	var nodes []*PlaylistNode

	for _, p := range playlists {
		node := &PlaylistNode{
			ID:    "navidrome-" + p.id,
			Title: p.name,
		}

		trows, err := db.Query(`
			SELECT mf.path FROM playlist_tracks pt
			JOIN media_file mf ON pt.media_file_id = mf.id
			WHERE pt.playlist_id = ?
		`, p.id)
		if err != nil {
			log.Printf("Navidrome: failed to query tracks for playlist %s: %v", p.name, err)
			continue
		}

		musicRoot := getMusicRoot()
		for trows.Next() {
			var relPath string
			if err := trows.Scan(&relPath); err != nil {
				continue
			}
			absPath := filepath.Join(musicRoot, relPath)

			libraryMu.RLock()
			for _, t := range allTracks {
				if t.Path == absPath {
					node.TrackIDs = append(node.TrackIDs, t.ID)
					break
				}
			}
			libraryMu.RUnlock()
		}
		trows.Close()

		if len(node.TrackIDs) > 0 {
			log.Printf("Navidrome: loaded playlist '%s' with %d tracks", p.name, len(node.TrackIDs))
			nodes = append(nodes, node)
		}
	}

	return nodes
}
