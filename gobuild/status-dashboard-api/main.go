package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/gorilla/mux"
	"gobuild/shared/kafka"
	"gobuild/shared/message"
)

type BuildStatus struct {
	ID            string    `json:"id"`
	RepositoryURL string    `json:"repository_url"`
	Branch        string    `json:"branch,omitempty"`
	CommitHash    string    `json:"commit_hash,omitempty"`
	Status        string    `json:"status"`
	Message       string    `json:"message,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	ArtifactURL   string    `json:"artifact_url,omitempty"`
	Logs          []string  `json:"logs,omitempty"`
}

type StatusDashboardAPI struct {
	redisClient *redis.Client
	builds      map[string]*BuildStatus
}

// NewStatusDashboardAPI creates a new StatusDashboardAPI
func NewStatusDashboardAPI(redisClient *redis.Client) *StatusDashboardAPI {
	return &StatusDashboardAPI{
		redisClient: redisClient,
		builds:      make(map[string]*BuildStatus),
	}
}

func (api *StatusDashboardAPI) GetBuilds(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()

	// First try to get from build:status:* keys
	statusKeys, err := api.redisClient.Keys(ctx, "build:status:*").Result()
	if err != nil {
		log.Printf("Failed to retrieve build status keys from Redis: %v", err)
	}

	// Also get from build:* keys (excluding build:status:*)
	buildKeys, err := api.redisClient.Keys(ctx, "build:*").Result()
	if err != nil {
		log.Printf("Failed to retrieve build keys from Redis: %v", err)
	}

	// Combine and deduplicate
	keyMap := make(map[string]bool)
	buildMap := make(map[string]*BuildStatus)

	// Process build:status:* keys first (these have the most up-to-date info)
	for _, key := range statusKeys {
		buildID := key[13:] // Remove "build:status:" prefix
		keyMap[buildID] = true

		buildJSON, err := api.redisClient.Get(ctx, key).Result()
		if err != nil {
			log.Printf("Failed to get build status %s: %v", key, err)
			continue
		}

		var build BuildStatus
		err = json.Unmarshal([]byte(buildJSON), &build)
		if err != nil {
			log.Printf("Failed to unmarshal build status %s: %v", key, err)
			continue
		}

		// Ensure ID is set
		if build.ID == "" {
			build.ID = buildID
		}

		buildMap[buildID] = &build
	}

	// Process build:* keys for any missing builds
	for _, key := range buildKeys {
		if len(key) > 6 && key[:6] == "build:" && key[:13] != "build:status:" {
			buildID := key[6:] // Remove "build:" prefix

			// Skip if we already have this build from status keys
			if keyMap[buildID] {
				continue
			}

			buildJSON, err := api.redisClient.Get(ctx, key).Result()
			if err != nil {
				log.Printf("Failed to get build %s: %v", key, err)
				continue
			}

			// Try to parse as BuildJob structure from orchestrator
			var buildJob struct {
				ID            string    `json:"id"`
				RepositoryURL string    `json:"repository_url"`
				Branch        string    `json:"branch"`
				CommitHash    string    `json:"commit_hash"`
				UserID        string    `json:"user_id"`
				Status        string    `json:"status"`
				CreatedAt     time.Time `json:"created_at"`
				UpdatedAt     time.Time `json:"updated_at"`
			}

			err = json.Unmarshal([]byte(buildJSON), &buildJob)
			if err != nil {
				log.Printf("Failed to unmarshal build %s: %v", key, err)
				continue
			}

			build := &BuildStatus{
				ID:            buildJob.ID,
				RepositoryURL: buildJob.RepositoryURL,
				Branch:        buildJob.Branch,
				CommitHash:    buildJob.CommitHash,
				Status:        buildJob.Status,
				CreatedAt:     buildJob.CreatedAt,
				UpdatedAt:     buildJob.UpdatedAt,
			}

			buildMap[buildID] = build
		}
	}

	// Convert map to slice
	builds := make([]*BuildStatus, 0, len(buildMap))
	for _, build := range buildMap {
		builds = append(builds, build)
	}

	// Sort builds by creation time (newest first)
	for i := 0; i < len(builds)-1; i++ {
		for j := 0; j < len(builds)-i-1; j++ {
			if builds[j].CreatedAt.Before(builds[j+1].CreatedAt) {
				builds[j], builds[j+1] = builds[j+1], builds[j]
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(builds)
}

func (api *StatusDashboardAPI) GetBuild(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	buildID := vars["buildId"]

	ctx := context.Background()

	// Try to get from build:status:* first
	var build BuildStatus
	buildFound := false

	buildJSON, err := api.redisClient.Get(ctx, "build:status:"+buildID).Result()
	if err == nil {
		err = json.Unmarshal([]byte(buildJSON), &build)
		if err == nil {
			buildFound = true
		}
	}

	// If not found, try regular build:* key
	if !buildFound {
		buildJSON, err = api.redisClient.Get(ctx, "build:"+buildID).Result()
		if err != nil {
			if err == redis.Nil {
				http.Error(w, "Build not found", http.StatusNotFound)
				return
			}
			log.Printf("Failed to retrieve build %s: %v", buildID, err)
			http.Error(w, "Failed to retrieve build", http.StatusInternalServerError)
			return
		}

		// Try to parse as BuildJob structure
		var buildJob struct {
			ID            string    `json:"id"`
			RepositoryURL string    `json:"repository_url"`
			Branch        string    `json:"branch"`
			CommitHash    string    `json:"commit_hash"`
			UserID        string    `json:"user_id"`
			Status        string    `json:"status"`
			CreatedAt     time.Time `json:"created_at"`
			UpdatedAt     time.Time `json:"updated_at"`
		}

		err = json.Unmarshal([]byte(buildJSON), &buildJob)
		if err != nil {
			log.Printf("Failed to parse build data for %s: %v", buildID, err)
			http.Error(w, "Failed to parse build data", http.StatusInternalServerError)
			return
		}

		build = BuildStatus{
			ID:            buildJob.ID,
			RepositoryURL: buildJob.RepositoryURL,
			Branch:        buildJob.Branch,
			CommitHash:    buildJob.CommitHash,
			Status:        buildJob.Status,
			CreatedAt:     buildJob.CreatedAt,
			UpdatedAt:     buildJob.UpdatedAt,
		}
	}

	// Get logs for this build
	logs, err := api.redisClient.LRange(ctx, "logs:"+buildID, 0, -1).Result()
	if err != nil && err != redis.Nil {
		log.Printf("Failed to get logs for build %s: %v", buildID, err)
	}
	build.Logs = logs

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(build)
}

// ProcessBuildStatus processes a build status update
func (api *StatusDashboardAPI) ProcessBuildStatus(statusMsg message.BuildStatusMessage) {
	ctx := context.Background()

	// Get existing build data
	var build BuildStatus
	buildFound := false

	// Try build:status:* first
	buildJSON, err := api.redisClient.Get(ctx, "build:status:"+statusMsg.BuildID).Result()
	if err == nil {
		err = json.Unmarshal([]byte(buildJSON), &build)
		if err == nil {
			buildFound = true
		}
	}

	// Try regular build:* key
	if !buildFound {
		buildJSON, err = api.redisClient.Get(ctx, "build:"+statusMsg.BuildID).Result()
		if err == nil {
			// Try to parse as BuildJob structure
			var buildJob struct {
				ID            string    `json:"id"`
				RepositoryURL string    `json:"repository_url"`
				Branch        string    `json:"branch"`
				CommitHash    string    `json:"commit_hash"`
				UserID        string    `json:"user_id"`
				Status        string    `json:"status"`
				CreatedAt     time.Time `json:"created_at"`
				UpdatedAt     time.Time `json:"updated_at"`
			}

			err = json.Unmarshal([]byte(buildJSON), &buildJob)
			if err == nil {
				build = BuildStatus{
					ID:            buildJob.ID,
					RepositoryURL: buildJob.RepositoryURL,
					Branch:        buildJob.Branch,
					CommitHash:    buildJob.CommitHash,
					Status:        buildJob.Status,
					CreatedAt:     buildJob.CreatedAt,
					UpdatedAt:     buildJob.UpdatedAt,
				}
				buildFound = true
			}
		}
	}

	// If still not found, create minimal build
	if !buildFound {
		build = BuildStatus{
			ID:        statusMsg.BuildID,
			Status:    statusMsg.Status,
			Message:   statusMsg.Message,
			CreatedAt: statusMsg.UpdatedAt,
			UpdatedAt: statusMsg.UpdatedAt,
		}
	} else {
		// Update existing build
		build.Status = statusMsg.Status
		build.Message = statusMsg.Message
		build.UpdatedAt = statusMsg.UpdatedAt
	}

	// Save the updated build to Redis
	buildJSONBytes, err := json.Marshal(build)
	if err != nil {
		log.Printf("Failed to marshal build %s: %v", statusMsg.BuildID, err)
		return
	}

	err = api.redisClient.Set(ctx, "build:status:"+statusMsg.BuildID, buildJSONBytes, 24*time.Hour).Err()
	if err != nil {
		log.Printf("Failed to save build %s: %v", statusMsg.BuildID, err)
		return
	}

	log.Printf("Updated build status: %s - %s", statusMsg.BuildID, statusMsg.Status)
}

func (api *StatusDashboardAPI) ProcessBuildLog(logMsg message.BuildLogMessage) {
	ctx := context.Background()

	err := api.redisClient.RPush(ctx, "logs:"+logMsg.BuildID, logMsg.LogEntry).Err()
	if err != nil {
		log.Printf("Failed to save log entry for build %s: %v", logMsg.BuildID, err)
		return
	}

	err = api.redisClient.Expire(ctx, "logs:"+logMsg.BuildID, 24*time.Hour).Err()
	if err != nil {
		log.Printf("Failed to set expiry for logs of build %s: %v", logMsg.BuildID, err)
		return
	}

	log.Printf("Saved log entry for build: %s", logMsg.BuildID)
}

// ProcessBuildCompletion processes a build completion message
func (api *StatusDashboardAPI) ProcessBuildCompletion(completionMsg message.BuildCompletionMessage) {
	ctx := context.Background()

	// Get existing build data
	var build BuildStatus
	buildFound := false

	// Try build:status:* first
	buildJSON, err := api.redisClient.Get(ctx, "build:status:"+completionMsg.BuildID).Result()
	if err == nil {
		err = json.Unmarshal([]byte(buildJSON), &build)
		if err == nil {
			buildFound = true
		}
	}

	// Try regular build:* key
	if !buildFound {
		buildJSON, err = api.redisClient.Get(ctx, "build:"+completionMsg.BuildID).Result()
		if err != nil {
			log.Printf("Failed to get build %s: %v", completionMsg.BuildID, err)
			return
		}

		// Try to parse as BuildJob structure
		var buildJob struct {
			ID            string    `json:"id"`
			RepositoryURL string    `json:"repository_url"`
			Branch        string    `json:"branch"`
			CommitHash    string    `json:"commit_hash"`
			UserID        string    `json:"user_id"`
			Status        string    `json:"status"`
			CreatedAt     time.Time `json:"created_at"`
			UpdatedAt     time.Time `json:"updated_at"`
		}

		err = json.Unmarshal([]byte(buildJSON), &buildJob)
		if err != nil {
			log.Printf("Failed to unmarshal build %s: %v", completionMsg.BuildID, err)
			return
		}

		build = BuildStatus{
			ID:            buildJob.ID,
			RepositoryURL: buildJob.RepositoryURL,
			Branch:        buildJob.Branch,
			CommitHash:    buildJob.CommitHash,
			Status:        buildJob.Status,
			CreatedAt:     buildJob.CreatedAt,
			UpdatedAt:     buildJob.UpdatedAt,
		}
	}

	// Update with completion data
	build.Status = completionMsg.Status
	build.UpdatedAt = completionMsg.CompletedAt
	build.ArtifactURL = completionMsg.ArtifactURL

	buildJSONBytes, err := json.Marshal(build)
	if err != nil {
		log.Printf("Failed to marshal build %s: %v", completionMsg.BuildID, err)
		return
	}

	err = api.redisClient.Set(ctx, "build:status:"+completionMsg.BuildID, buildJSONBytes, 24*time.Hour).Err()
	if err != nil {
		log.Printf("Failed to save build %s: %v", completionMsg.BuildID, err)
		return
	}

	log.Printf("Updated build completion: %s - %s", completionMsg.BuildID, completionMsg.Status)
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8086"
	}

	redisClient := redis.NewClient(&redis.Options{
		Addr: "redis:6379",
	})
	defer redisClient.Close()

	kafkaConsumer, err := kafka.NewConsumer("kafka:29092", "status-dashboard-api")
	if err != nil {
		log.Fatalf("Failed to create Kafka consumer: %v", err)
	}
	defer kafkaConsumer.Close()

	err = kafkaConsumer.Subscribe([]string{"build-status", "build-logs", "build-completions"})
	if err != nil {
		log.Fatalf("Failed to subscribe to topics: %v", err)
	}

	api := NewStatusDashboardAPI(redisClient)

	go func() {
		kafkaConsumer.ConsumeMessages(func(key, value []byte) error {
			// Try to unmarshal as different message types
			var statusMsg message.BuildStatusMessage
			if err := kafka.UnmarshalMessage(value, &statusMsg); err == nil && statusMsg.BuildID != "" {
				log.Printf("Received status message for build: %s", statusMsg.BuildID)
				api.ProcessBuildStatus(statusMsg)
				return nil
			}

			var logMsg message.BuildLogMessage
			if err := kafka.UnmarshalMessage(value, &logMsg); err == nil && logMsg.BuildID != "" {
				log.Printf("Received log message for build: %s", logMsg.BuildID)
				api.ProcessBuildLog(logMsg)
				return nil
			}

			var completionMsg message.BuildCompletionMessage
			if err := kafka.UnmarshalMessage(value, &completionMsg); err == nil && completionMsg.BuildID != "" {
				log.Printf("Received completion message for build: %s", completionMsg.BuildID)
				api.ProcessBuildCompletion(completionMsg)
				return nil
			}

			log.Printf("Unknown message type received")
			return nil
		})
	}()

	r := mux.NewRouter()

	// Add CORS middleware to all routes
	r.Use(corsMiddleware)

	// Handle OPTIONS for all routes
	r.Methods("OPTIONS").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.WriteHeader(http.StatusOK)
	})

	r.HandleFunc("/api/builds", api.GetBuilds).Methods("GET")
	r.HandleFunc("/api/builds/{buildId}", api.GetBuild).Methods("GET")

	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	log.Printf("Status Dashboard API is running on port %s...", port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}
