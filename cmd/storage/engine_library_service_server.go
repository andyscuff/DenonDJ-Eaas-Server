package main

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"sort"
	"strings"

	"github.com/icedream/go-stagelinq/eaas/proto/enginelibrary"
	"golang.org/x/text/unicode/norm"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var _ enginelibrary.EngineLibraryServiceServer = &EngineLibraryServiceServer{}

type EngineLibraryServiceServer struct {
	enginelibrary.UnimplementedEngineLibraryServiceServer
}

const mainLibraryID = "cubi-music-library"
const mainLibraryName = "Cubi Music"

func (e *EngineLibraryServiceServer) EventStream(ctx context.Context, req *enginelibrary.EventStreamRequest) (*enginelibrary.EventStreamResponse, error) {
	log.Printf("EventStream: %+v", req)
	return &enginelibrary.EventStreamResponse{
		Event: []*enginelibrary.Event{},
	}, nil
}

func (e *EngineLibraryServiceServer) GetCredentials(ctx context.Context, req *enginelibrary.GetCredentialsRequest) (*enginelibrary.GetCredentialsResponse, error) {
	log.Printf("GetCredentials: %+v", req)
	panic("unimplemented")
}

func (e *EngineLibraryServiceServer) GetHistoryPlayedTracks(ctx context.Context, req *enginelibrary.GetHistoryPlayedTracksRequest) (*enginelibrary.GetHistoryPlayedTracksResponse, error) {
	return &enginelibrary.GetHistoryPlayedTracksResponse{Tracks: []*enginelibrary.HistoryPlayedTrack{}}, nil
}

func (e *EngineLibraryServiceServer) GetHistorySessions(ctx context.Context, req *enginelibrary.GetHistorySessionsRequest) (*enginelibrary.GetHistorySessionsResponse, error) {
	return &enginelibrary.GetHistorySessionsResponse{Sessions: []*enginelibrary.HistorySession{}}, nil
}

func (e *EngineLibraryServiceServer) GetLibraries(ctx context.Context, req *enginelibrary.GetLibrariesRequest) (*enginelibrary.GetLibrariesResponse, error) {
	log.Printf("GetLibraries: %+v", req)
	id := mainLibraryID
	name := mainLibraryName
	return &enginelibrary.GetLibrariesResponse{
		Libraries: []*enginelibrary.Library{{Id: &id, Title: &name}},
	}, nil
}

func (e *EngineLibraryServiceServer) GetLibrary(ctx context.Context, req *enginelibrary.GetLibraryRequest) (*enginelibrary.GetLibraryResponse, error) {
	log.Printf("GetLibrary: %+v", req)
	libraryMu.RLock()
	defer libraryMu.RUnlock()
	resp := &enginelibrary.GetLibraryResponse{}
	for _, p := range allPlaylists {
		resp.Playlists = append(resp.Playlists, playlistToProto(p))
	}
	return resp, nil
}

// distinctFilterValues collects the distinct, sorted values of a field
// across the library, for populating the Genre/Artist/Album/etc. browse
// columns on the hardware — those columns are driven entirely by
// GetSearchFilters, not derived from the Playlists tree.
func distinctFilterValues(tracks []*Track, get func(*Track) string) []*enginelibrary.SearchFilterValue {
	seen := map[string]bool{}
	for _, t := range tracks {
		if v := get(t); v != "" {
			seen[v] = true
		}
	}
	values := make([]string, 0, len(seen))
	for v := range seen {
		values = append(values, v)
	}
	sort.Strings(values)
	out := make([]*enginelibrary.SearchFilterValue, len(values))
	for i, v := range values {
		vv := v
		out[i] = &enginelibrary.SearchFilterValue{Value: &vv}
	}
	return out
}

func (e *EngineLibraryServiceServer) GetSearchFilters(ctx context.Context, req *enginelibrary.GetSearchFiltersRequest) (*enginelibrary.GetSearchFiltersResponse, error) {
	libraryMu.RLock()
	defer libraryMu.RUnlock()

	// The browse columns only offer values that actually occur among tracks
	// matching whatever's currently in the search box — same "Clap your"
	// text that filters the track list on the right narrows Genre/Artist/
	// Album on the left too, not just the track list.
	query := ""
	if req.Query != nil {
		query = strings.TrimSpace(*req.Query)
	}
	tracks := allTracks
	if len(query) >= 2 {
		tracks = nil
		for _, t := range allTracks {
			if trackMatchesQuery(t, query) {
				tracks = append(tracks, t)
			}
		}
	}

	return &enginelibrary.GetSearchFiltersResponse{
		SearchFilters: &enginelibrary.SearchFilterOptions{
			Genres:  distinctFilterValues(tracks, func(t *Track) string { return t.Genre }),
			Artists: distinctFilterValues(tracks, func(t *Track) string { return t.Artist }),
			Albums:  distinctFilterValues(tracks, func(t *Track) string { return t.Album }),
		},
	}, nil
}

func (e *EngineLibraryServiceServer) GetTrack(ctx context.Context, req *enginelibrary.GetTrackRequest) (*enginelibrary.GetTrackResponse, error) {
	log.Printf("GetTrack: %+v", req)
	libraryMu.RLock()
	defer libraryMu.RUnlock()
	for _, t := range allTracks {
		id := fmt.Sprintf("%d", t.ID)
		if id == req.GetTrackId() {
			url := trackURL(t)
			size := trackFileSize(t)
			metadata := trackToMetadata(t)
			return &enginelibrary.GetTrackResponse{
				Blob: &enginelibrary.TrackBlob{
					Type: &enginelibrary.TrackBlob_Url{
						Url: &enginelibrary.TrackBlobUrl{
							Url:      &url,
							FileSize: &size,
						},
					},
				},
				Metadata: metadata,
				PerformanceData: &enginelibrary.TrackPerformanceData{
					Bpm: metadata.Bpm,
					MainCue: &enginelibrary.MainCue{
						Position:        &unsetFloat64,
						InitialPosition: &unsetFloat64,
					},
				},
			}, nil
		}
	}
	return nil, status.Error(codes.NotFound, "track not found")
}

func (e *EngineLibraryServiceServer) GetTracks(ctx context.Context, req *enginelibrary.GetTracksRequest) (*enginelibrary.GetTracksResponse, error) {
	log.Printf("GetTracks: %+v", req)
	libraryMu.RLock()
	defer libraryMu.RUnlock()
	resp := &enginelibrary.GetTracksResponse{Tracks: []*enginelibrary.ListTrack{}}

	playlistID := req.GetPlaylistId()
	if playlistID != "" {
		log.Printf("GetTracks: incoming playlist_id raw bytes: %x (string: %q)", []byte(playlistID), playlistID)
		normalizedID := norm.NFC.String(playlistID)
		log.Printf("GetTracks: NFC-normalized bytes: %x (string: %q)", []byte(normalizedID), normalizedID)
		playlistID = normalizedID
		if p, ok := playlistMap[playlistID]; ok {
			limit := int(req.GetPageSize())
			if limit <= 0 {
				limit = 25
			}
			for _, tid := range p.TrackIDs {
				if len(resp.Tracks) >= limit {
					break
				}
				if t, ok := trackMap[tid]; ok {
					lt := &enginelibrary.ListTrack{Metadata: trackToMetadata(t)}
					if len(t.Artwork) > 0 {
						lt.PreviewArtwork = t.Artwork
					}
					resp.Tracks = append(resp.Tracks, lt)
				}
			}
			return resp, nil
		}
		log.Printf("GetTracks: playlist_id %q not found in map (%d entries)", playlistID, len(playlistMap))
		n := 0
		for k := range playlistMap {
			log.Printf("  map key[%d]: %x (string: %q)", n, []byte(k), k)
			n++
			if n >= 20 {
				log.Printf("  ... (%d more keys omitted)", len(playlistMap)-20)
				break
			}
		}
		return resp, nil
	}

	filters := req.GetFilters()
	if len(filters) > 0 {
		limit := int(req.GetPageSize())
		if limit <= 0 {
			limit = 25
		}
		for _, t := range allTracks {
			if len(resp.Tracks) >= limit {
				break
			}
			if !trackMatchesFilters(t, filters) {
				continue
			}
			lt := &enginelibrary.ListTrack{Metadata: trackToMetadata(t)}
			if len(t.Artwork) > 0 {
				lt.PreviewArtwork = t.Artwork
			}
			resp.Tracks = append(resp.Tracks, lt)
		}
		return resp, nil
	}

	// Return empty for root collection view
	return resp, nil
}

// trackMatchesFilters implements the browse-column drill-down (Genre then
// Artist then Album, each narrowing the next): AND across different filter
// fields, OR within one field's repeated values. BPM/Key filters have no
// backing data in Track yet, so a filter on either field matches everything
// rather than excluding every track.
func trackMatchesFilters(t *Track, filters []*enginelibrary.SearchFilter) bool {
	for _, f := range filters {
		if !trackMatchesFilter(t, f) {
			return false
		}
	}
	return true
}

func trackMatchesFilter(t *Track, f *enginelibrary.SearchFilter) bool {
	var field string
	switch f.GetField() {
	case enginelibrary.SearchFilterField_SEARCH_FILTER_FIELD_GENRE:
		field = t.Genre
	case enginelibrary.SearchFilterField_SEARCH_FILTER_FIELD_ARTIST:
		field = t.Artist
	case enginelibrary.SearchFilterField_SEARCH_FILTER_FIELD_ALBUM:
		field = t.Album
	default:
		return true
	}
	for _, v := range f.GetValue() {
		if strings.EqualFold(field, v) {
			return true
		}
	}
	return false
}

func (e *EngineLibraryServiceServer) PutEvents(ctx context.Context, req *enginelibrary.PutEventsRequest) (*enginelibrary.PutEventsResponse, error) {
	return &enginelibrary.PutEventsResponse{}, nil
}

func trackMatchesQuery(t *Track, q string) bool {
	q = strings.ToLower(q)
	filenameNoExt := strings.ToLower(strings.TrimSuffix(t.Filename, filepath.Ext(t.Filename)))
	return strings.Contains(strings.ToLower(t.Title), q) ||
		strings.Contains(strings.ToLower(t.Artist), q) ||
		strings.Contains(strings.ToLower(t.Album), q) ||
		strings.Contains(strings.ToLower(t.Genre), q) ||
		strings.Contains(filenameNoExt, q)
}

func (e *EngineLibraryServiceServer) SearchTracks(ctx context.Context, req *enginelibrary.SearchTracksRequest) (*enginelibrary.SearchTracksResponse, error) {
	log.Printf("SearchTracks: %+v", req)
	libraryMu.RLock()
	defer libraryMu.RUnlock()
	resp := &enginelibrary.SearchTracksResponse{Tracks: []*enginelibrary.ListTrack{}}

	query := ""
	if req.Query != nil {
		query = strings.TrimSpace(*req.Query)
	}

	// Require at least 2 characters to search
	if len(query) < 2 {
		return resp, nil
	}

	filters := req.GetFilters()

	// Respect page_size, default to 50 max
	limit := int(req.GetPageSize())
	if limit <= 0 || limit > 50 {
		limit = 50
	}

	for _, t := range allTracks {
		if len(resp.Tracks) >= limit {
			break
		}
		if !trackMatchesQuery(t, query) || !trackMatchesFilters(t, filters) {
			continue
		}
		lt := &enginelibrary.ListTrack{Metadata: trackToMetadata(t)}
		if len(t.Artwork) > 0 {
			lt.PreviewArtwork = t.Artwork
		}
		resp.Tracks = append(resp.Tracks, lt)
	}

	log.Printf("SearchTracks: returning %d results for query '%s'", len(resp.Tracks), query)
	return resp, nil
}
