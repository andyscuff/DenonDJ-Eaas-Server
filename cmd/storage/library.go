package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/dhowden/tag"
	"github.com/icedream/go-stagelinq/eaas/proto/enginelibrary"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var unsetFloat64 float64 = -1

type Track struct {
	ID       int
	Path     string
	Filename string
	Title    string
	Artist   string
	Album    string
	Genre    string
	BPM      float64
	Year     int
	Length   int
	Label    string
	Comment  string
	Composer string
	Remixer  string
	Artwork  []byte
}

type PlaylistNode struct {
	ID       string
	Title    string
	Children []*PlaylistNode
	TrackIDs []int
}

var (
	libraryMu    sync.RWMutex
	allTracks    []*Track
	allPlaylists []*PlaylistNode
	trackMap     map[int]*Track
	playlistMap  map[string]*PlaylistNode
)

func isAudioFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".flac" || ext == ".mp3" || ext == ".aiff" || ext == ".wav" || ext == ".m4a"
}

func readTrackTags(trackPath string, t *Track) {
	f, err := os.Open(trackPath)
	if err != nil {
		return
	}
	defer f.Close()
	m, err := tag.ReadFrom(f)
	if err != nil {
		return
	}
	if m.Title() != "" { t.Title = m.Title() }
	if m.Artist() != "" { t.Artist = m.Artist() }
	if m.Album() != "" { t.Album = m.Album() }
	if m.Genre() != "" { t.Genre = m.Genre() }
	if m.Year() != 0 { t.Year = m.Year() }
	if m.Picture() != nil { t.Artwork = m.Picture().Data }
}

func loadLibrary(path string) error {
	newTrackMap := make(map[int]*Track)
	newPlaylistMap := make(map[string]*PlaylistNode)
	var newAllTracks []*Track
	var newAllPlaylists []*PlaylistNode
	trackID := 0

	genreDirs, err := os.ReadDir(path)
	if err != nil {
		return fmt.Errorf("read music root: %w", err)
	}

	totalGenres := 0
	for _, d := range genreDirs {
		if d.IsDir() && !strings.HasPrefix(d.Name(), ".") {
			totalGenres++
		}
	}
	genreCount := 0

	for _, genreDir := range genreDirs {
		if !genreDir.IsDir() || strings.HasPrefix(genreDir.Name(), ".") {
			continue
		}
		genreCount++
		log.Printf("Scanning genre %d/%d: %s", genreCount, totalGenres, genreDir.Name())

		genrePath := filepath.Join(path, genreDir.Name())
		genreNode := &PlaylistNode{
			ID:    "genre-" + genreDir.Name(),
			Title: genreDir.Name(),
		}

		artistDirs, err := os.ReadDir(genrePath)
		if err != nil {
			continue
		}

		for _, artistDir := range artistDirs {
			if !artistDir.IsDir() {
				continue
			}
			artistPath := filepath.Join(genrePath, artistDir.Name())
			artistNode := &PlaylistNode{
				ID:    "artist-" + genreDir.Name() + "-" + artistDir.Name(),
				Title: artistDir.Name(),
			}

			albumDirs, err := os.ReadDir(artistPath)
			if err != nil {
				continue
			}

			hasDirectTracks := false
			for _, entry := range albumDirs {
				if !entry.IsDir() && isAudioFile(entry.Name()) {
					hasDirectTracks = true
					break
				}
			}

			if hasDirectTracks {
				albumNode := &PlaylistNode{
					ID:    "album-" + genreDir.Name() + "-" + artistDir.Name() + "-" + artistDir.Name(),
					Title: artistDir.Name(),
				}
				for _, trackFile := range albumDirs {
					if trackFile.IsDir() || !isAudioFile(trackFile.Name()) {
						continue
					}
					trackPath := filepath.Join(artistPath, trackFile.Name())
					t := &Track{
						ID:       trackID,
						Path:     trackPath,
						Filename: trackFile.Name(),
						Title:    strings.TrimSuffix(trackFile.Name(), filepath.Ext(trackFile.Name())),
						Artist:   artistDir.Name(),
						Album:    artistDir.Name(),
						Genre:    genreDir.Name(),
					}
					readTrackTags(trackPath, t)
					newAllTracks = append(newAllTracks, t)
					newTrackMap[trackID] = t
					albumNode.TrackIDs = append(albumNode.TrackIDs, trackID)
					trackID++
				}
				if len(albumNode.TrackIDs) > 0 {
					artistNode.Children = append(artistNode.Children, albumNode)
					newPlaylistMap[albumNode.ID] = albumNode
				}
			}

			for _, albumDir := range albumDirs {
				if !albumDir.IsDir() {
					continue
				}
				albumPath := filepath.Join(artistPath, albumDir.Name())
				albumNode := &PlaylistNode{
					ID:    "album-" + genreDir.Name() + "-" + artistDir.Name() + "-" + albumDir.Name(),
					Title: albumDir.Name(),
				}
				trackFiles, err := os.ReadDir(albumPath)
				if err != nil {
					continue
				}
				for _, trackFile := range trackFiles {
					if trackFile.IsDir() || !isAudioFile(trackFile.Name()) {
						continue
					}
					trackPath := filepath.Join(albumPath, trackFile.Name())
					t := &Track{
						ID:       trackID,
						Path:     trackPath,
						Filename: trackFile.Name(),
						Title:    strings.TrimSuffix(trackFile.Name(), filepath.Ext(trackFile.Name())),
						Artist:   artistDir.Name(),
						Album:    albumDir.Name(),
						Genre:    genreDir.Name(),
					}
					readTrackTags(trackPath, t)
					newAllTracks = append(newAllTracks, t)
					newTrackMap[trackID] = t
					albumNode.TrackIDs = append(albumNode.TrackIDs, trackID)
					trackID++
				}
				if len(albumNode.TrackIDs) > 0 {
					artistNode.Children = append(artistNode.Children, albumNode)
					newPlaylistMap[albumNode.ID] = albumNode
				}
			}

			if len(artistNode.Children) > 0 {
				genreNode.Children = append(genreNode.Children, artistNode)
				newPlaylistMap[artistNode.ID] = artistNode
			}
		}

		if len(genreNode.Children) > 0 {
			newAllPlaylists = append(newAllPlaylists, genreNode)
			newPlaylistMap[genreNode.ID] = genreNode
		}
	}

	libraryMu.Lock()
	allTracks = newAllTracks
	allPlaylists = newAllPlaylists
	trackMap = newTrackMap
	playlistMap = newPlaylistMap
	libraryMu.Unlock()

	log.Printf("Loaded %d tracks across %d playlists", len(newAllTracks), len(newPlaylistMap))

	ndPlaylists := loadNavidromePlaylists()
	if len(ndPlaylists) > 0 {
		myPlaylists := &PlaylistNode{
			ID:       "my-playlists",
			Title:    "My Playlists",
			Children: ndPlaylists,
		}
		for _, p := range ndPlaylists {
			newPlaylistMap[p.ID] = p
		}
		newPlaylistMap["my-playlists"] = myPlaylists

		libraryMu.Lock()
		allPlaylists = append(allPlaylists, myPlaylists)
		playlistMap = newPlaylistMap
		libraryMu.Unlock()

		log.Printf("Loaded %d Navidrome playlists", len(ndPlaylists))
	}

	return nil
}

func trackToMetadata(t *Track) *enginelibrary.TrackMetadata {
	id := fmt.Sprintf("%d", t.ID)
	m := &enginelibrary.TrackMetadata{
		Id:        &id,
		DateAdded: timestamppb.Now(),
	}
	if t.Title != "" { m.Title = &t.Title }
	if t.Artist != "" { m.Artist = &t.Artist }
	if t.Album != "" { m.Album = &t.Album }
	if t.Genre != "" { m.Genre = &t.Genre }
	if t.BPM > 0 { m.Bpm = &t.BPM }
	if t.Year > 0 { y := uint32(t.Year); m.Year = &y }
	if t.Label != "" { m.Label = &t.Label }
	if t.Comment != "" { m.Comment = &t.Comment }
	if t.Composer != "" { m.Composer = &t.Composer }
	if t.Remixer != "" { m.Remixer = &t.Remixer }
	if t.Length > 0 { l := uint32(t.Length); m.LengthSeconds = &l }
	return m
}

func trackArtworkURL(t *Track) string {
	if len(t.Artwork) == 0 {
		return ""
	}
	return fmt.Sprintf("%s/artwork/%d", getArtworkBaseURL(), t.ID)
}

func trackURL(t *Track) string {
	winPath := strings.ReplaceAll(t.Path, "/", "\\")
	return fmt.Sprintf("<C:\\%s>", winPath[1:])
}

func trackFileSize(t *Track) uint32 {
	info, err := os.Stat(t.Path)
	if err != nil {
		return 0
	}
	return uint32(info.Size())
}

func playlistToProto(p *PlaylistNode) *enginelibrary.PlaylistMetadata {
	count := uint32(len(p.TrackIDs))
	listType := enginelibrary.ListType_LIST_TYPE_PLAY
	proto := &enginelibrary.PlaylistMetadata{
		Id:         &p.ID,
		Title:      &p.Title,
		TrackCount: &count,
		ListType:   &listType,
	}
	for _, child := range p.Children {
		proto.Playlists = append(proto.Playlists, playlistToProto(child))
	}
	return proto
}
