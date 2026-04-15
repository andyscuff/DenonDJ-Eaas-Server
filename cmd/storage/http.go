package main

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
)

func logMuxHandling(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("Mux, handling: %s %s", r.Method, r.URL.String())
		h.ServeHTTP(w, r)
	})
}

func handleNotFound(w http.ResponseWriter, r *http.Request) {
	log.Printf("Mux, not found: %s %s", r.Method, r.URL.String())
	w.WriteHeader(http.StatusNotFound)
}

func handlePing(w http.ResponseWriter, r *http.Request) {
	log.Println("HTTP: Ping")
	w.WriteHeader(http.StatusOK)
}

func handleDownload(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	requestedPath, err := url.PathUnescape(vars["path"])
	if err != nil {
		log.Println("HTTP: Download, bad path:", requestedPath)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	linuxPath := requestedPath
	linuxPath = strings.Trim(linuxPath, "<>")
	linuxPath = strings.TrimPrefix(linuxPath, "C:")
	linuxPath = strings.ReplaceAll(linuxPath, "\\", "/")

	log.Println("HTTP: Download, serving:", linuxPath)
	http.ServeFile(w, r, linuxPath)
}

func handleArtwork(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	idStr := vars["id"]
	id, err := strconv.Atoi(idStr)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	libraryMu.RLock()
	t, ok := trackMap[id]
	libraryMu.RUnlock()

	if !ok || len(t.Artwork) == 0 {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	// Detect image type
	contentType := "image/jpeg"
	if len(t.Artwork) > 4 && string(t.Artwork[:4]) == "\x89PNG" {
		contentType = "image/png"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(t.Artwork)))
	w.WriteHeader(http.StatusOK)
	bytes.NewReader(t.Artwork).WriteTo(w)
}

func eaasHTTPHandler() http.Handler {
	r := mux.NewRouter()
	r.Use(logMuxHandling)
	r.NotFoundHandler = http.HandlerFunc(handleNotFound)
	r.UseEncodedPath()
	r.SkipClean(true)
	r.HandleFunc("/download/{path}", handleDownload).Methods(http.MethodGet)
	r.HandleFunc("/artwork/{id}", handleArtwork).Methods(http.MethodGet)
	r.HandleFunc("/ping", handlePing).Methods(http.MethodGet)
	return r
}
