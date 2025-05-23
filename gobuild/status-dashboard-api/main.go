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

	keys, err := api.redisClient.Keys(ctx, "build:*").Result()
	if err != nil {
		log.Printf("Failed to retrieve builds from Redis: %v", err)
		http.Error(w, "Failed to retrieve builds", http.StatusInternalServerError)
		return
	}

	builds := make([]*BuildStatus, 0, len(keys))

	for _, key := range keys {
		buildJSON, err := api.redisClient.Get(ctx, key).Result()
		if err != nil {
			log.Printf("Failed to get build %s: %v", key, err)
			continue
		}

		var build BuildStatus
		err = json.Unmarshal([]byte(buildJSON), &build)
		if err != nil {
			log.Printf("Failed to unmarshal build %s: %v", key, err)
			continue
		}

		builds = append(builds, &build)
	}

	// Sort builds by creation time (newest first)
	// Simple bubble sort for demonstration
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

	var build BuildStatus
	err = json.Unmarshal([]byte(buildJSON), &build)
	if err != nil {
		log.Printf("Failed to parse build data for %s: %v", buildID, err)
		http.Error(w, "Failed to parse build data", http.StatusInternalServerError)
		return
	}

	// Get logs for this build
	logs, err := api.redisClient.LRange(ctx, "logs:"+buildID, 0, -1).Result()
	if err != nil && err != redis.Nil {
		log.Printf("Failed to get logs for build %s: %v", buildID, err)
		// Don't fail the request if logs can't be retrieved
	}
	build.Logs = logs

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(build)
}

// ProcessBuildStatus processes a build status update
func (api *StatusDashboardAPI) ProcessBuildStatus(statusMsg message.BuildStatusMessage) {
	ctx := context.Background()

	buildJSON, err := api.redisClient.Get(ctx, "build:"+statusMsg.BuildID).Result()
	if err != nil && err != redis.Nil {
		log.Printf("Failed to get build %s: %v", statusMsg.BuildID, err)
		return
	}

	var build BuildStatus
	if err == redis.Nil {
		// Build doesn't exist yet, create a new one
		build = BuildStatus{
			ID:        statusMsg.BuildID,
			Status:    statusMsg.Status,
			Message:   statusMsg.Message,
			CreatedAt: statusMsg.UpdatedAt,
			UpdatedAt: statusMsg.UpdatedAt,
		}
	} else {
		// Update existing build
		err = json.Unmarshal([]byte(buildJSON), &build)
		if err != nil {
			log.Printf("Failed to unmarshal build %s: %v", statusMsg.BuildID, err)
			return
		}

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

	err = api.redisClient.Set(ctx, "build:"+statusMsg.BuildID, buildJSONBytes, 24*time.Hour).Err()
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

	buildJSON, err := api.redisClient.Get(ctx, "build:"+completionMsg.BuildID).Result()
	if err != nil {
		log.Printf("Failed to get build %s: %v", completionMsg.BuildID, err)
		return
	}

	var build BuildStatus
	err = json.Unmarshal([]byte(buildJSON), &build)
	if err != nil {
		log.Printf("Failed to unmarshal build %s: %v", completionMsg.BuildID, err)
		return
	}

	build.Status = completionMsg.Status
	build.UpdatedAt = completionMsg.CompletedAt
	build.ArtifactURL = completionMsg.ArtifactURL

	buildJSONBytes, err := json.Marshal(build)
	if err != nil {
		log.Printf("Failed to marshal build %s: %v", completionMsg.BuildID, err)
		return
	}

	err = api.redisClient.Set(ctx, "build:"+completionMsg.BuildID, buildJSONBytes, 24*time.Hour).Err()
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
