package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gorilla/mux"
)

type StorageService struct {
	artifactsDir string
}

func NewStorageService(artifactsDir string) *StorageService {
	return &StorageService{
		artifactsDir: artifactsDir,
	}
}

func (s *StorageService) GetArtifact(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	buildID := vars["buildId"]

	if buildID == "" {
		http.Error(w, "Build ID is required", http.StatusBadRequest)
		return
	}

	artifactPath := filepath.Join(s.artifactsDir, fmt.Sprintf("%s.tar.gz", buildID))

	if _, err := os.Stat(artifactPath); os.IsNotExist(err) {
		http.Error(w, "Artifact not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s.tar.gz", buildID))

	http.ServeFile(w, r, artifactPath)
}

func (s *StorageService) UploadArtifact(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	buildID := vars["buildId"]

	if buildID == "" {
		http.Error(w, "Build ID is required", http.StatusBadRequest)
		return
	}

	err := r.ParseMultipartForm(32 << 20) // 32MB max
	if err != nil {
		http.Error(w, "Error parsing form: "+err.Error(), http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile("artifact")
	if err != nil {
		http.Error(w, "Error retrieving file: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()

	artifactPath := filepath.Join(s.artifactsDir, fmt.Sprintf("%s.tar.gz", buildID))

	dst, err := os.Create(artifactPath)
	if err != nil {
		http.Error(w, "Error creating file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	_, err = io.Copy(dst, file)
	if err != nil {
		http.Error(w, "Error saving file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Artifact uploaded successfully")
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8083"
	}

	artifactsDir := "/app/artifacts"
	err := os.MkdirAll(artifactsDir, 0755)
	if err != nil {
		log.Fatalf("Failed to create artifacts directory: %v", err)
	}

	storage := NewStorageService(artifactsDir)

	r := mux.NewRouter()

	r.HandleFunc("/artifacts/{buildId}", storage.GetArtifact).Methods("GET")

	r.HandleFunc("/artifacts/{buildId}", storage.UploadArtifact).Methods("POST")

	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	log.Printf("Storage Service is running on port %s...", port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}
