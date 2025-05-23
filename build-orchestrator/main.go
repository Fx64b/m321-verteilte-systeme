package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/gorilla/mux"
	"gobuild/shared/kafka"
	"gobuild/shared/message"
	"gobuild/shared/model"
)

type BuildOrchestrator struct {
	mutex         sync.RWMutex
	kafkaProducer *kafka.Producer
	redisClient   *redis.Client
}

func NewBuildOrchestrator(kafkaProducer *kafka.Producer, redisClient *redis.Client) *BuildOrchestrator {
	return &BuildOrchestrator{
		kafkaProducer: kafkaProducer,
		redisClient:   redisClient,
	}
}

// ProcessBuildRequest creates a new build
func (bo *BuildOrchestrator) ProcessBuildRequest(buildReq message.BuildRequestMessage) error {
	log.Printf("🔄 Processing build request: %s for repo: %s", buildReq.ID, buildReq.RepositoryURL)

	// Create build status
	buildStatus := &model.BuildStatus{
		ID:            buildReq.ID,
		RepositoryURL: buildReq.RepositoryURL,
		Branch:        buildReq.Branch,
		CommitHash:    buildReq.CommitHash,
		UserID:        buildReq.UserID,
		Status:        "queued",
		Message:       "Build queued for processing",
		CreatedAt:     buildReq.CreatedAt,
		UpdatedAt:     time.Now(),
	}

	// Store in Redis (single source of truth)
	if err := bo.storeBuildStatus(buildStatus); err != nil {
		log.Printf("❌ Failed to store build status: %v", err)
		return err
	}

	// Send status update via Kafka
	statusMsg := message.BuildStatusMessage{
		BuildID:   buildStatus.ID,
		Status:    buildStatus.Status,
		Message:   buildStatus.Message,
		UpdatedAt: buildStatus.UpdatedAt,
	}
	if err := bo.kafkaProducer.SendMessage("build-status", buildStatus.ID, statusMsg); err != nil {
		log.Printf("❌ Failed to send status update: %v", err)
		return err
	}

	// Forward to builder
	if err := bo.kafkaProducer.SendMessage("build-jobs", buildReq.ID, buildReq); err != nil {
		log.Printf("❌ Failed to send to build-jobs topic: %v", err)
		return err
	}

	log.Printf("✅ Build %s created and queued", buildReq.ID)
	return nil
}

// ProcessBuildStatus updates build status
func (bo *BuildOrchestrator) ProcessBuildStatus(statusMsg message.BuildStatusMessage) error {
	log.Printf("📊 Processing build status update: %s - %s", statusMsg.BuildID, statusMsg.Status)

	// Get existing build
	buildStatus, err := bo.getBuildStatus(statusMsg.BuildID)
	if err != nil {
		log.Printf("❌ Failed to get build status: %v", err)
		return err
	}

	// Update fields
	buildStatus.Status = statusMsg.Status
	buildStatus.Message = statusMsg.Message
	buildStatus.UpdatedAt = statusMsg.UpdatedAt

	// Set StartedAt if transitioning to in-progress
	if statusMsg.Status == "in-progress" && buildStatus.StartedAt == nil {
		now := time.Now()
		buildStatus.StartedAt = &now
	}

	// Store updated status
	if err := bo.storeBuildStatus(buildStatus); err != nil {
		log.Printf("❌ Failed to update build status: %v", err)
		return err
	}

	return nil
}

// ProcessBuildCompletion handles build completion
func (bo *BuildOrchestrator) ProcessBuildCompletion(completionMsg message.BuildCompletionMessage) error {
	log.Printf("🏁 Processing build completion: %s - %s", completionMsg.BuildID, completionMsg.Status)
	log.Printf("📦 Artifact URL: %s", completionMsg.ArtifactURL)

	buildStatus, err := bo.getBuildStatus(completionMsg.BuildID)
	if err != nil {
		log.Printf("❌ Failed to get build status: %v", err)
		return err
	}

	buildStatus.Status = completionMsg.Status
	buildStatus.ArtifactURL = completionMsg.ArtifactURL
	buildStatus.UpdatedAt = completionMsg.CompletedAt
	buildStatus.CompletedAt = &completionMsg.CompletedAt
	buildStatus.Duration = completionMsg.Duration

	if err := bo.storeBuildStatus(buildStatus); err != nil {
		log.Printf("❌ Failed to store updated build status: %v", err)
		return err
	}

	// Send status update
	statusUpdate := message.BuildStatusMessage{
		BuildID:   completionMsg.BuildID,
		Status:    completionMsg.Status,
		Message:   fmt.Sprintf("Build %s", completionMsg.Status),
		UpdatedAt: completionMsg.CompletedAt,
	}

	return bo.kafkaProducer.SendMessage("build-status", completionMsg.BuildID, statusUpdate)
}

// storeBuildStatus stores build status in Redis with proper locking
func (bo *BuildOrchestrator) storeBuildStatus(buildStatus *model.BuildStatus) error {
	ctx := context.Background()
	key := fmt.Sprintf("build:%s", buildStatus.ID)

	// Use Redis transaction for atomic update
	pipe := bo.redisClient.TxPipeline()

	buildJSON, err := json.Marshal(buildStatus)
	if err != nil {
		return err
	}

	pipe.Set(ctx, key, buildJSON, 24*time.Hour)

	// Also update a sorted set for efficient listing
	pipe.ZAdd(ctx, "builds:by_date", &redis.Z{
		Score:  float64(buildStatus.CreatedAt.Unix()),
		Member: buildStatus.ID,
	})

	_, err = pipe.Exec(ctx)
	return err
}

// getBuildStatus retrieves build status from Redis
func (bo *BuildOrchestrator) getBuildStatus(buildID string) (*model.BuildStatus, error) {
	ctx := context.Background()
	key := fmt.Sprintf("build:%s", buildID)

	buildJSON, err := bo.redisClient.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("build not found: %s", buildID)
		}
		return nil, err
	}

	var buildStatus model.BuildStatus
	if err := json.Unmarshal([]byte(buildJSON), &buildStatus); err != nil {
		return nil, err
	}

	return &buildStatus, nil
}

// GetBuildJob retrieves a build for API requests
func (bo *BuildOrchestrator) GetBuildJob(buildID string) (*model.BuildStatus, error) {
	return bo.getBuildStatus(buildID)
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8082"
	}

	log.Println("🚀 Starting Build Orchestrator...")

	kafkaProducer, err := kafka.NewProducer("kafka:29092")
	if err != nil {
		log.Fatalf("❌ Failed to create Kafka producer: %v", err)
	}
	defer kafkaProducer.Close()
	log.Println("✅ Kafka producer created")

	redisClient := redis.NewClient(&redis.Options{
		Addr: "redis:6379",
	})
	defer redisClient.Close()
	log.Println("✅ Redis client created")

	// Test Redis connection
	ctx := context.Background()
	_, err = redisClient.Ping(ctx).Result()
	if err != nil {
		log.Fatalf("❌ Failed to connect to Redis: %v", err)
	}
	log.Println("✅ Redis connection verified")

	orchestrator := NewBuildOrchestrator(kafkaProducer, redisClient)

	kafkaConsumer, err := kafka.NewConsumer("kafka:29092", "build-orchestrator")
	if err != nil {
		log.Fatalf("❌ Failed to create Kafka consumer: %v", err)
	}
	defer kafkaConsumer.Close()

	// Subscribe to all relevant topics
	err = kafkaConsumer.Subscribe([]string{"build-requests", "build-status", "build-completions"})
	if err != nil {
		log.Fatalf("❌ Failed to subscribe to topics: %v", err)
	}
	log.Println("✅ Subscribed to topics: build-requests, build-status, build-completions")

	// Start consuming messages
	go func() {
		log.Println("🎧 Starting to consume messages...")
		kafkaConsumer.ConsumeMessages(func(key, value []byte) error {
			// Try to process as build request
			var buildReq message.BuildRequestMessage
			if err := kafka.UnmarshalMessage(value, &buildReq); err == nil && buildReq.ID != "" {
				log.Printf("📬 Received build request: %s", buildReq.ID)
				return orchestrator.ProcessBuildRequest(buildReq)
			}

			// Try to process as completion
			var completionMsg message.BuildCompletionMessage
			if err := kafka.UnmarshalMessage(value, &completionMsg); err == nil && completionMsg.BuildID != "" && completionMsg.ArtifactURL != "" {
				log.Printf("🏁 Received completion: %s - %s", completionMsg.BuildID, completionMsg.Status)
				return orchestrator.ProcessBuildCompletion(completionMsg)
			}

			// Try to process as status update
			var statusMsg message.BuildStatusMessage
			if err := kafka.UnmarshalMessage(value, &statusMsg); err == nil && statusMsg.BuildID != "" {
				return orchestrator.ProcessBuildStatus(statusMsg)
			}

			log.Printf("⚠️ Received unknown message type")
			return nil
		})
	}()

	r := mux.NewRouter()

	r.HandleFunc("/api/builds/{buildId}", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		buildID := vars["buildId"]

		job, err := orchestrator.GetBuildJob(buildID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(job)
	}).Methods("GET")

	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	log.Printf("🌐 Build Orchestrator Service is running on port %s...", port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}
