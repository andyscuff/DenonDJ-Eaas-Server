package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dhowden/tag"
	"github.com/icedream/go-stagelinq/eaas/proto/enginelibrary"
	"golang.org/x/text/unicode/norm"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const recentlyAddedLimit = 50

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
	AddedAt  time.Time
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

// rawTagKeys maps our fields to the various raw tag keys different container
// formats use for them (ID3v2.2/2.3/2.4 frame IDs, lowercase Vorbis comment
// field names). dhowden/tag doesn't expose these via its Metadata interface.
var (
	bpmTagKeys     = []string{"TBP", "TBPM", "bpm"}
	labelTagKeys   = []string{"TPB", "TPUB", "label", "organization", "publisher"}
	remixerTagKeys = []string{"TP4", "TPE4", "remixer", "modifiedby"}
)

func rawTagString(raw map[string]interface{}, keys []string) string {
	for _, k := range keys {
		v, ok := raw[k]
		if !ok {
			continue
		}
		if s, ok := v.(string); ok && s != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func readTrackTags(trackPath string, t *Track) {
	if info, err := os.Stat(trackPath); err == nil {
		t.AddedAt = info.ModTime()
	}
	t.Length = trackDuration(trackPath)

	f, err := os.Open(trackPath)
	if err != nil {
		return
	}
	defer f.Close()
	m, err := tag.ReadFrom(f)
	if err != nil {
		return
	}
	if m.Title() != "" {
		t.Title = m.Title()
	}
	if m.Artist() != "" {
		t.Artist = m.Artist()
	}
	if m.Album() != "" {
		t.Album = m.Album()
	}
	if m.Genre() != "" {
		t.Genre = m.Genre()
	}
	if m.Year() != 0 {
		t.Year = m.Year()
	}
	if m.Comment() != "" {
		t.Comment = m.Comment()
	}
	if m.Composer() != "" {
		t.Composer = m.Composer()
	}
	if m.Picture() != nil {
		t.Artwork = m.Picture().Data
	}

	raw := m.Raw()
	if bpmStr := rawTagString(raw, bpmTagKeys); bpmStr != "" {
		if bpm, err := strconv.ParseFloat(strings.TrimRight(bpmStr, "\x00"), 64); err == nil {
			t.BPM = bpm
		}
	} else if tmpo, ok := raw["tmpo"]; ok {
		if v, ok := tmpo.(int); ok {
			t.BPM = float64(v)
		}
	}
	if label := rawTagString(raw, labelTagKeys); label != "" {
		t.Label = strings.TrimRight(label, "\x00")
	}
	if remixer := rawTagString(raw, remixerTagKeys); remixer != "" {
		t.Remixer = strings.TrimRight(remixer, "\x00")
	}
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

		genreName := norm.NFC.String(genreDir.Name())
		genrePath := filepath.Join(path, genreDir.Name())
		genreNode := &PlaylistNode{
			ID:    "genre-" + genreName,
			Title: genreName,
		}

		artistDirs, err := os.ReadDir(genrePath)
		if err != nil {
			continue
		}

		for _, artistDir := range artistDirs {
			if !artistDir.IsDir() {
				continue
			}
			artistName := norm.NFC.String(artistDir.Name())
			artistPath := filepath.Join(genrePath, artistDir.Name())
			artistNode := &PlaylistNode{
				ID:    "artist-" + genreName + "-" + artistName,
				Title: artistName,
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
					ID:    "album-" + genreName + "-" + artistName + "-" + artistName,
					Title: artistName,
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
						Artist:   artistName,
						Album:    artistName,
						Genre:    genreName,
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
					log.Printf("scan: inserted direct-album key bytes: %x (string: %q)", []byte(albumNode.ID), albumNode.ID)
				}
			}

			for _, albumDir := range albumDirs {
				if !albumDir.IsDir() {
					continue
				}
				albumName := norm.NFC.String(albumDir.Name())
				albumPath := filepath.Join(artistPath, albumDir.Name())
				albumNode := &PlaylistNode{
					ID:    "album-" + genreName + "-" + artistName + "-" + albumName,
					Title: albumName,
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
						Artist:   artistName,
						Album:    albumName,
						Genre:    genreName,
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
					log.Printf("scan: inserted album key bytes: %x (string: %q)", []byte(albumNode.ID), albumNode.ID)
				}
			}

			if len(artistNode.Children) > 0 {
				genreNode.Children = append(genreNode.Children, artistNode)
				newPlaylistMap[artistNode.ID] = artistNode
				log.Printf("scan: inserted artist key bytes: %x (string: %q)", []byte(artistNode.ID), artistNode.ID)
			}
		}

		if len(genreNode.Children) > 0 {
			newAllPlaylists = append(newAllPlaylists, genreNode)
			newPlaylistMap[genreNode.ID] = genreNode
			log.Printf("scan: inserted genre key bytes: %x (string: %q)", []byte(genreNode.ID), genreNode.ID)
		}
	}

	if recent := buildRecentlyAddedPlaylist(newAllTracks); recent != nil {
		newAllPlaylists = append([]*PlaylistNode{recent}, newAllPlaylists...)
		newPlaylistMap[recent.ID] = recent
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

func buildRecentlyAddedPlaylist(tracks []*Track) *PlaylistNode {
	if len(tracks) == 0 {
		return nil
	}
	sorted := make([]*Track, len(tracks))
	copy(sorted, tracks)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].AddedAt.After(sorted[j].AddedAt)
	})

	limit := recentlyAddedLimit
	if limit > len(sorted) {
		limit = len(sorted)
	}
	node := &PlaylistNode{ID: "recently-added", Title: "Recently Added"}
	for _, t := range sorted[:limit] {
		node.TrackIDs = append(node.TrackIDs, t.ID)
	}
	return node
}

func trackToMetadata(t *Track) *enginelibrary.TrackMetadata {
	id := fmt.Sprintf("%d", t.ID)
	dateAdded := t.AddedAt
	if dateAdded.IsZero() {
		dateAdded = time.Now()
	}
	m := &enginelibrary.TrackMetadata{
		Id:        &id,
		DateAdded: timestamppb.New(dateAdded),
	}
	if t.Title != "" {
		m.Title = &t.Title
	}
	if t.Artist != "" {
		m.Artist = &t.Artist
	}
	if t.Album != "" {
		m.Album = &t.Album
	}
	if t.Genre != "" {
		m.Genre = &t.Genre
	}
	if t.BPM > 0 {
		m.Bpm = &t.BPM
	}
	if t.Year > 0 {
		y := uint32(t.Year)
		m.Year = &y
	}
	if t.Label != "" {
		m.Label = &t.Label
	}
	if t.Comment != "" {
		m.Comment = &t.Comment
	}
	if t.Composer != "" {
		m.Composer = &t.Composer
	}
	if t.Remixer != "" {
		m.Remixer = &t.Remixer
	}
	if t.Length > 0 {
		l := uint32(t.Length)
		m.LengthSeconds = &l
	}
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
