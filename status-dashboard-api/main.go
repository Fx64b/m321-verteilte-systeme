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
	"gobuild/shared/model"
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
}

func NewStatusDashboardAPI(redisClient *redis.Client) *StatusDashboardAPI {
	return &StatusDashboardAPI{
		redisClient: redisClient,
	}
}

func (api *StatusDashboardAPI) GetBuilds(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()

	// Get build IDs from sorted set (newest first)
	buildIDs, err := api.redisClient.ZRevRange(ctx, "builds:by_date", 0, 99).Result()
	if err != nil {
		log.Printf("Failed to get build IDs: %v", err)
		buildIDs = []string{} // Fallback to empty list
	}

	builds := make([]*model.BuildStatus, 0)

	// If no builds in sorted set, try pattern matching as fallback
	if len(buildIDs) == 0 {
		keys, err := api.redisClient.Keys(ctx, "build:*").Result()
		if err != nil {
			log.Printf("Failed to get build keys: %v", err)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(builds)
			return
		}

		for _, key := range keys {
			if len(key) > 6 { // "build:" is 6 chars
				buildIDs = append(buildIDs, key[6:])
			}
		}
	}

	// Retrieve each build
	for _, buildID := range buildIDs {
		buildJSON, err := api.redisClient.Get(ctx, "build:"+buildID).Result()
		if err != nil {
			continue
		}

		var build model.BuildStatus
		if err := json.Unmarshal([]byte(buildJSON), &build); err != nil {
			continue
		}

		builds = append(builds, &build)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(builds)
}

func (api *StatusDashboardAPI) GetBuild(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	buildID := vars["buildId"]

	ctx := context.Background()

	buildJSON, err := api.redisClient.Get(ctx, "build:"+buildID).Result()
	if err != nil {
		if err == redis.Nil {
			http.Error(w, "Build not found", http.StatusNotFound)
			return
		}
		log.Printf("Failed to retrieve build %s: %v", buildID, err)
		http.Error(w, "Failed to retrieve build", http.StatusInternalServerError)
		return
	}

	var build model.BuildStatus
	if err := json.Unmarshal([]byte(buildJSON), &build); err != nil {
		log.Printf("Failed to parse build data for %s: %v", buildID, err)
		http.Error(w, "Failed to parse build data", http.StatusInternalServerError)
		return
	}

	// Get logs for this build
	logs, err := api.redisClient.LRange(ctx, "logs:"+buildID, 0, -1).Result()
	if err != nil && err != redis.Nil {
		log.Printf("Failed to get logs for build %s: %v", buildID, err)
	}

	// Add logs to response
	response := struct {
		*model.BuildStatus
		Logs []string `json:"logs,omitempty"`
	}{
		BuildStatus: &build,
		Logs:        logs,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
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
}

// ProcessBuildLog stores build logs (still needed for log storage)
func (api *StatusDashboardAPI) ProcessBuildLog(logMsg message.BuildLogMessage) {
	ctx := context.Background()

	err := api.redisClient.RPush(ctx, "logs:"+logMsg.BuildID, logMsg.LogEntry).Err()
	if err != nil {
		log.Printf("Failed to save log entry for build %s: %v", logMsg.BuildID, err)
		return
	}

	// Set expiry on logs
	api.redisClient.Expire(ctx, "logs:"+logMsg.BuildID, 24*time.Hour)
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
			var logMsg message.BuildLogMessage
			if err := kafka.UnmarshalMessage(value, &logMsg); err == nil && logMsg.BuildID != "" {
				api.ProcessBuildLog(logMsg)
				return nil
			}
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
