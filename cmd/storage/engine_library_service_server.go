package main

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"github.com/icedream/go-stagelinq/eaas/proto/enginelibrary"
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

func (e *EngineLibraryServiceServer) GetSearchFilters(ctx context.Context, req *enginelibrary.GetSearchFiltersRequest) (*enginelibrary.GetSearchFiltersResponse, error) {
	return &enginelibrary.GetSearchFiltersResponse{
		SearchFilters: &enginelibrary.SearchFilterOptions{},
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
			return &enginelibrary.GetTrackResponse{
				Blob: &enginelibrary.TrackBlob{
					Type: &enginelibrary.TrackBlob_Url{
						Url: &enginelibrary.TrackBlobUrl{
							Url:      &url,
							FileSize: &size,
						},
					},
				},
				Metadata: trackToMetadata(t),
				PerformanceData: &enginelibrary.TrackPerformanceData{
					Bpm: trackToMetadata(t).Bpm,
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
		if p, ok := playlistMap[playlistID]; ok {
			for _, tid := range p.TrackIDs {
				if t, ok := trackMap[tid]; ok {
					lt := &enginelibrary.ListTrack{
						Metadata: trackToMetadata(t),
					}
					if len(t.Artwork) > 0 {
						lt.PreviewArtwork = t.Artwork
					}
					resp.Tracks = append(resp.Tracks, lt)
				}
			}
			return resp, nil
		}
		return resp, nil
	}

	// Return empty for root collection view
	return resp, nil
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
	for _, t := range allTracks {
		if req.Query != nil && *req.Query != "" {
			if !trackMatchesQuery(t, *req.Query) {
				continue
			}
		}
		lt := &enginelibrary.ListTrack{
			Metadata: trackToMetadata(t),
		}
		if len(t.Artwork) > 0 {
			lt.PreviewArtwork = t.Artwork
		}
		resp.Tracks = append(resp.Tracks, lt)
	}
	return resp, nil
}

