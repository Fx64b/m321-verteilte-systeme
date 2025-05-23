package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/go-redis/redis/v8"
	"github.com/gorilla/mux"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gobuild/shared/kafka"
	"gobuild/shared/message"
)

// Builder executes build jobs
type Builder struct {
	id            string
	workDir       string
	kafkaProducer *kafka.Producer
	storageURL    string
	redisClient   *redis.Client
}

// NewBuilder creates a new Builder
func NewBuilder(id, workDir, storageURL string, kafkaProducer *kafka.Producer, redisClient *redis.Client) *Builder {
	return &Builder{
		id:            id,
		workDir:       workDir,
		kafkaProducer: kafkaProducer,
		storageURL:    storageURL,
		redisClient:   redisClient,
	}
}

func (b *Builder) storeBuildLogInRedis(buildID string, logEntry string) error {
	ctx := context.Background()

	// Store log in Redis list
	err := b.redisClient.RPush(ctx, "logs:"+buildID, logEntry).Err()
	if err != nil {
		log.Printf("❌ Failed to store log in Redis: %v", err)
		return err
	}

	// Set expiry on logs
	err = b.redisClient.Expire(ctx, "logs:"+buildID, 24*time.Hour).Err()
	if err != nil {
		log.Printf("⚠️ Failed to set expiry on logs: %v", err)
	}

	return nil
}

func (b *Builder) updateBuildStatusInRedis(buildID string, status, message string, artifactURL string) error {
	ctx := context.Background()

	// Try to get existing build data from Redis
	var buildStatus map[string]interface{}

	// Check if build:status:{buildID} exists
	buildJSON, err := b.redisClient.Get(ctx, "build:status:"+buildID).Result()
	if err == nil {
		// Unmarshal existing data
		err = json.Unmarshal([]byte(buildJSON), &buildStatus)
		if err != nil {
			log.Printf("❌ Failed to unmarshal build status: %v", err)
			// Initialize empty map if unmarshal fails
			buildStatus = make(map[string]interface{})
		}
	} else if err == redis.Nil {
		// Key doesn't exist - check build:{buildID}
		buildJSON, err = b.redisClient.Get(ctx, "build:"+buildID).Result()
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
				// Convert to map
				buildStatus = map[string]interface{}{
					"id":             buildJob.ID,
					"repository_url": buildJob.RepositoryURL,
					"branch":         buildJob.Branch,
					"commit_hash":    buildJob.CommitHash,
					"status":         buildJob.Status,
					"created_at":     buildJob.CreatedAt,
					"updated_at":     buildJob.UpdatedAt,
				}
			} else {
				log.Printf("❌ Failed to unmarshal build job: %v", err)
				buildStatus = make(map[string]interface{})
			}
		} else {
			// Neither key exists - create new empty status
			buildStatus = make(map[string]interface{})
			// Add created_at for new builds
			buildStatus["created_at"] = time.Now()
		}
	} else {
		// Other error occurred
		log.Printf("❌ Error retrieving build status from Redis: %v", err)
		buildStatus = make(map[string]interface{})
		buildStatus["created_at"] = time.Now()
	}

	// Update fields
	buildStatus["id"] = buildID
	buildStatus["status"] = status
	buildStatus["updated_at"] = time.Now()

	// Only update these fields if they have values
	if message != "" {
		buildStatus["message"] = message
	}
	if artifactURL != "" {
		buildStatus["artifact_url"] = artifactURL
	}

	// Marshal and store
	buildStatusJSON, err := json.Marshal(buildStatus)
	if err != nil {
		log.Printf("❌ Failed to marshal build status: %v", err)
		return err
	}

	err = b.redisClient.Set(ctx, "build:status:"+buildID, buildStatusJSON, 24*time.Hour).Err()
	if err != nil {
		log.Printf("❌ Failed to store build status in Redis: %v", err)
		return err
	}

	return nil
}

func (b *Builder) sendLogLines(buildID string, logContent string) {
	lines := strings.Split(strings.TrimSpace(logContent), "\n")
	for _, line := range lines {
		if line != "" {
			// Send to Kafka
			logMsg := message.BuildLogMessage{
				BuildID:   buildID,
				LogEntry:  line,
				Timestamp: time.Now(),
			}
			b.kafkaProducer.SendMessage("build-logs", buildID, logMsg)

			// Store in Redis
			b.storeBuildLogInRedis(buildID, line)
		}
	}
}

// ProcessBuildJob processes a build job
func (b *Builder) ProcessBuildJob(buildReq message.BuildRequestMessage) error {
	log.Printf("🔨 Processing build request: %s for repo: %s", buildReq.ID, buildReq.RepositoryURL)

	// Send initial log
	b.sendLogLines(buildReq.ID, "Build started")
	b.sendLogLines(buildReq.ID, fmt.Sprintf("Repository: %s", buildReq.RepositoryURL))
	if buildReq.Branch != "" {
		b.sendLogLines(buildReq.ID, fmt.Sprintf("Branch: %s", buildReq.Branch))
	}

	// Send status update: in-progress
	b.updateBuildStatusInRedis(buildReq.ID, "in-progress", "Build started", "")
	statusMsg := message.BuildStatusMessage{
		BuildID:   buildReq.ID,
		Status:    "in-progress",
		Message:   "Build started",
		UpdatedAt: time.Now(),
	}
	err := b.kafkaProducer.SendMessage("build-status", buildReq.ID, statusMsg)
	if err != nil {
		log.Printf("❌ Failed to send status update: %v", err)
		return err
	}

	buildDir := filepath.Join(b.workDir, buildReq.ID)
	err = os.MkdirAll(buildDir, 0755)
	if err != nil {
		log.Printf("❌ Failed to create build directory: %v", err)
		return b.failBuild(buildReq.ID, fmt.Sprintf("Failed to create build directory: %v", err))
	}

	// Clean up build directory when done
	defer func() {
		if err := os.RemoveAll(buildDir); err != nil {
			log.Printf("⚠️ Failed to clean up build directory: %v", err)
		}
	}()

	b.sendLogLines(buildReq.ID, "Cloning repository...")

	cloneCmd := exec.Command("git", "clone", buildReq.RepositoryURL, buildDir)
	cloneCmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0") // Disable interactive prompts

	var cloneOutput bytes.Buffer
	cloneCmd.Stdout = &cloneOutput
	cloneCmd.Stderr = &cloneOutput

	if err := cloneCmd.Run(); err != nil {
		errorMsg := fmt.Sprintf("Clone failed: %s", err.Error())
		b.sendLogLines(buildReq.ID, errorMsg)
		b.sendLogLines(buildReq.ID, cloneOutput.String())
		return b.failBuild(buildReq.ID, "Failed to clone repository: "+err.Error())
	}

	// Log successful clone
	b.sendLogLines(buildReq.ID, "Repository cloned successfully")
	b.sendLogLines(buildReq.ID, cloneOutput.String())

	// Checkout specific branch if specified
	if buildReq.Branch != "" && buildReq.Branch != "main" && buildReq.Branch != "master" {
		b.sendLogLines(buildReq.ID, fmt.Sprintf("Checking out branch: %s", buildReq.Branch))

		checkoutCmd := exec.Command("git", "checkout", buildReq.Branch)
		checkoutCmd.Dir = buildDir

		var checkoutOutput bytes.Buffer
		checkoutCmd.Stdout = &checkoutOutput
		checkoutCmd.Stderr = &checkoutOutput

		if err := checkoutCmd.Run(); err != nil {
			errorMsg := fmt.Sprintf("Checkout failed: %s", err.Error())
			b.sendLogLines(buildReq.ID, errorMsg)
			b.sendLogLines(buildReq.ID, checkoutOutput.String())
			return b.failBuild(buildReq.ID, "Failed to checkout branch: "+err.Error())
		}

		b.sendLogLines(buildReq.ID, "Branch checked out successfully")
		b.sendLogLines(buildReq.ID, checkoutOutput.String())
	}

	// Execute build process
	if err := b.executeBuild(buildReq, buildDir); err != nil {
		return b.failBuild(buildReq.ID, err.Error())
	}

	// Create and upload artifact
	if err := b.createAndUploadArtifact(buildReq, buildDir); err != nil {
		return b.failBuild(buildReq.ID, "Failed to create artifact: "+err.Error())
	}

	b.sendLogLines(buildReq.ID, "Build completed successfully!")

	// Send completion message
	completionMsg := message.BuildCompletionMessage{
		BuildID:     buildReq.ID,
		Status:      "success",
		ArtifactURL: fmt.Sprintf("/artifacts/%s", buildReq.ID),
		Duration:    time.Since(buildReq.CreatedAt).Milliseconds(),
		CompletedAt: time.Now(),
	}
	err = b.kafkaProducer.SendMessage("build-completions", buildReq.ID, completionMsg)
	if err != nil {
		return err
	}

	statusMsg = message.BuildStatusMessage{
		BuildID:   buildReq.ID,
		Status:    "completed",
		Message:   "Build completed successfully",
		UpdatedAt: time.Now(),
	}
	return b.kafkaProducer.SendMessage("build-status", buildReq.ID, statusMsg)
}

// executeBuild runs the appropriate build command based on project type
func (b *Builder) executeBuild(buildReq message.BuildRequestMessage, buildDir string) error {
	buildFilePath := filepath.Join(buildDir, "build.sh")

	if _, err := os.Stat(buildFilePath); os.IsNotExist(err) {
		projectType := b.detectProjectType(buildDir)
		return b.buildByProjectType(buildReq, buildDir, projectType)
	} else {
		return b.runBuildScript(buildReq, buildDir)
	}
}

// buildByProjectType builds based on detected project type
func (b *Builder) buildByProjectType(buildReq message.BuildRequestMessage, buildDir, projectType string) error {
	switch projectType {
	case "node":
		return b.buildNodeProject(buildReq, buildDir)
	case "go":
		return b.buildGoProject(buildReq, buildDir)
	default:
		return fmt.Errorf("unknown project type: %s, no build script found", projectType)
	}
}

// buildNodeProject builds a Node.js project
func (b *Builder) buildNodeProject(buildReq message.BuildRequestMessage, buildDir string) error {
	b.sendLogLines(buildReq.ID, "Detected Node.js project")

	packageManager := b.detectNodePackageManager(buildDir)
	if packageManager == "unknown" {
		packageManager = "npm"
	}

	b.sendLogLines(buildReq.ID, fmt.Sprintf("Using package manager: %s", packageManager))

	// Install dependencies
	b.sendLogLines(buildReq.ID, fmt.Sprintf("Running %s install...", packageManager))
	installCmd := exec.Command(packageManager, "install")
	installCmd.Dir = buildDir

	var installOutput bytes.Buffer
	installCmd.Stdout = &installOutput
	installCmd.Stderr = &installOutput

	if err := installCmd.Run(); err != nil {
		errorMsg := fmt.Sprintf("%s install failed: %s", packageManager, err.Error())
		b.sendLogLines(buildReq.ID, errorMsg)
		b.sendLogLines(buildReq.ID, installOutput.String())
		return fmt.Errorf("failed to install dependencies: %s", err.Error())
	}

	b.sendLogLines(buildReq.ID, "Dependencies installed successfully")
	b.sendLogLines(buildReq.ID, installOutput.String())

	// Build project
	b.sendLogLines(buildReq.ID, fmt.Sprintf("Running %s run build...", packageManager))
	buildCmd := exec.Command(packageManager, "run", "build")
	buildCmd.Dir = buildDir

	var buildOutput bytes.Buffer
	buildCmd.Stdout = &buildOutput
	buildCmd.Stderr = &buildOutput

	if err := buildCmd.Run(); err != nil {
		errorMsg := fmt.Sprintf("%s build failed: %s", packageManager, err.Error())
		b.sendLogLines(buildReq.ID, errorMsg)
		b.sendLogLines(buildReq.ID, buildOutput.String())
		return fmt.Errorf("failed to build project: %s", err.Error())
	}

	b.sendLogLines(buildReq.ID, "Project built successfully")
	b.sendLogLines(buildReq.ID, buildOutput.String())

	return nil
}

// buildGoProject builds a Go project
func (b *Builder) buildGoProject(buildReq message.BuildRequestMessage, buildDir string) error {
	logMsg := message.BuildLogMessage{
		BuildID:   buildReq.ID,
		LogEntry:  "Detected Go project, running go build",
		Timestamp: time.Now(),
	}
	b.kafkaProducer.SendMessage("build-logs", buildReq.ID, logMsg)

	goBuildCmd := exec.Command("go", "build", "-o", "app")
	goBuildCmd.Dir = buildDir

	var buildOutput bytes.Buffer
	goBuildCmd.Stdout = &buildOutput
	goBuildCmd.Stderr = &buildOutput

	if err := goBuildCmd.Run(); err != nil {
		errorMsg := fmt.Sprintf("go build failed: %s\nOutput: %s", err.Error(), buildOutput.String())
		logMsg := message.BuildLogMessage{
			BuildID:   buildReq.ID,
			LogEntry:  errorMsg,
			Timestamp: time.Now(),
		}
		b.kafkaProducer.SendMessage("build-logs", buildReq.ID, logMsg)
		return fmt.Errorf("failed to build project: %s", err.Error())
	}

	logMsg = message.BuildLogMessage{
		BuildID:   buildReq.ID,
		LogEntry:  fmt.Sprintf("Go project built successfully\n%s", buildOutput.String()),
		Timestamp: time.Now(),
	}
	b.kafkaProducer.SendMessage("build-logs", buildReq.ID, logMsg)

	return nil
}

// runBuildScript executes a custom build script
func (b *Builder) runBuildScript(buildReq message.BuildRequestMessage, buildDir string) error {
	logMsg := message.BuildLogMessage{
		BuildID:   buildReq.ID,
		LogEntry:  "Executing build script",
		Timestamp: time.Now(),
	}
	b.kafkaProducer.SendMessage("build-logs", buildReq.ID, logMsg)

	buildCmd := exec.Command("/bin/sh", "build.sh")
	buildCmd.Dir = buildDir

	var buildOutput bytes.Buffer
	buildCmd.Stdout = &buildOutput
	buildCmd.Stderr = &buildOutput

	if err := buildCmd.Run(); err != nil {
		errorMsg := fmt.Sprintf("Build script failed: %s\nOutput: %s", err.Error(), buildOutput.String())
		logMsg := message.BuildLogMessage{
			BuildID:   buildReq.ID,
			LogEntry:  errorMsg,
			Timestamp: time.Now(),
		}
		b.kafkaProducer.SendMessage("build-logs", buildReq.ID, logMsg)
		return fmt.Errorf("build script failed: %s", err.Error())
	}

	logMsg = message.BuildLogMessage{
		BuildID:   buildReq.ID,
		LogEntry:  fmt.Sprintf("Build script completed successfully\n%s", buildOutput.String()),
		Timestamp: time.Now(),
	}
	b.kafkaProducer.SendMessage("build-logs", buildReq.ID, logMsg)

	return nil
}

// createAndUploadArtifact creates a tar.gz of the build and uploads it
func (b *Builder) createAndUploadArtifact(buildReq message.BuildRequestMessage, buildDir string) error {
	logMsg := message.BuildLogMessage{
		BuildID:   buildReq.ID,
		LogEntry:  "Creating artifact...",
		Timestamp: time.Now(),
	}
	b.kafkaProducer.SendMessage("build-logs", buildReq.ID, logMsg)

	// Create a temporary artifact file
	tempArtifactPath := filepath.Join(b.workDir, fmt.Sprintf("%s.tar.gz", buildReq.ID))

	tarCmd := exec.Command("tar", "-czf", tempArtifactPath, ".")
	tarCmd.Dir = buildDir

	var tarOutput bytes.Buffer
	tarCmd.Stdout = &tarOutput
	tarCmd.Stderr = &tarOutput

	if err := tarCmd.Run(); err != nil {
		errorMsg := fmt.Sprintf("Failed to create artifact: %s\nOutput: %s", err.Error(), tarOutput.String())
		logMsg := message.BuildLogMessage{
			BuildID:   buildReq.ID,
			LogEntry:  errorMsg,
			Timestamp: time.Now(),
		}
		b.kafkaProducer.SendMessage("build-logs", buildReq.ID, logMsg)
		return fmt.Errorf("failed to create artifact: %s", err.Error())
	}

	// Clean up temporary artifact when done
	defer os.Remove(tempArtifactPath)

	// Upload artifact to storage service
	logMsg = message.BuildLogMessage{
		BuildID:   buildReq.ID,
		LogEntry:  "Uploading artifact to storage...",
		Timestamp: time.Now(),
	}
	b.kafkaProducer.SendMessage("build-logs", buildReq.ID, logMsg)

	if err := b.uploadArtifact(buildReq.ID, tempArtifactPath); err != nil {
		logMsg := message.BuildLogMessage{
			BuildID:   buildReq.ID,
			LogEntry:  fmt.Sprintf("Failed to upload artifact: %v", err),
			Timestamp: time.Now(),
		}
		b.kafkaProducer.SendMessage("build-logs", buildReq.ID, logMsg)
		return fmt.Errorf("failed to upload artifact: %s", err.Error())
	}

	logMsg = message.BuildLogMessage{
		BuildID:   buildReq.ID,
		LogEntry:  "Artifact uploaded successfully",
		Timestamp: time.Now(),
	}
	b.kafkaProducer.SendMessage("build-logs", buildReq.ID, logMsg)

	return nil
}

// uploadArtifact uploads the artifact to the storage service
func (b *Builder) uploadArtifact(buildID, artifactPath string) error {
	log.Printf("📦 Uploading artifact %s to storage service...", artifactPath)
	// Open the artifact file
	file, err := os.Open(artifactPath)
	if err != nil {
		return fmt.Errorf("failed to open artifact file: %v", err)
	}
	defer file.Close()

	// Create a buffer to store our request body
	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)

	// Create a form file field
	part, err := writer.CreateFormFile("artifact", fmt.Sprintf("%s.tar.gz", buildID))
	if err != nil {
		return fmt.Errorf("failed to create form file: %v", err)
	}

	// Copy the file content to the form field
	_, err = io.Copy(part, file)
	if err != nil {
		return fmt.Errorf("failed to copy file content: %v", err)
	}

	// Close the multipart writer
	err = writer.Close()
	if err != nil {
		return fmt.Errorf("failed to close multipart writer: %v", err)
	}

	// Create the HTTP request
	url := fmt.Sprintf("%s/artifacts/%s", b.storageURL, buildID)
	req, err := http.NewRequest("POST", url, &requestBody)
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	// Set the content type header
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Send the request
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to upload artifact: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("storage service returned status %d: %s", resp.StatusCode, string(body))
	} else {
		artifactURL := fmt.Sprintf("%s/artifacts/%s", b.storageURL, buildID)

		// Update Redis with the artifact URL
		err = b.updateBuildStatusInRedis(buildID, "completed", "", artifactURL)
		if err != nil {
			log.Printf("⚠️ Failed to update build status in Redis: %v", err)
		}

		// Create and send build completion message via Kafka
		completionMessage := message.BuildCompletionMessage{
			BuildID:     buildID,
			Status:      "success",
			ArtifactURL: artifactURL,
			CompletedAt: time.Now(),
		}

		err = b.kafkaProducer.SendMessage("build-completions", buildID, completionMessage)
		if err != nil {
			log.Printf("⚠️ Failed to send build completion message: %v", err)
		} else {
			log.Printf("📤 Sent build completion message for build %s", buildID)
		}

		log.Printf("✅ Artifact uploaded successfully to %s", url)
		return nil
	}
}

// failBuild handles build failures
func (b *Builder) failBuild(buildID, errorMsg string) error {
	log.Printf("❌ Build failed for %s: %s", buildID, errorMsg)

	// Send failure log
	logMsg := message.BuildLogMessage{
		BuildID:   buildID,
		LogEntry:  fmt.Sprintf("Build failed: %s", errorMsg),
		Timestamp: time.Now(),
	}
	b.kafkaProducer.SendMessage("build-logs", buildID, logMsg)
	b.storeBuildLogInRedis(buildID, fmt.Sprintf("Build failed: %s", errorMsg))

	completionMsg := message.BuildCompletionMessage{
		BuildID:     buildID,
		Status:      "failure",
		ArtifactURL: "",
		Duration:    0,
		CompletedAt: time.Now(),
	}
	err := b.kafkaProducer.SendMessage("build-completions", buildID, completionMsg)
	if err != nil {
		return err
	}

	statusMsg := message.BuildStatusMessage{
		BuildID:   buildID,
		Status:    "failed",
		Message:   errorMsg,
		UpdatedAt: time.Now(),
	}
	return b.kafkaProducer.SendMessage("build-status", buildID, statusMsg)
}

// detectProjectType attempts to determine the type of project in the directory
func (b *Builder) detectProjectType(dir string) string {
	// Check for package.json (Node.js)
	if _, err := os.Stat(filepath.Join(dir, "package.json")); !os.IsNotExist(err) {
		return "node"
	}

	// Check for go.mod (Go)
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); !os.IsNotExist(err) {
		return "go"
	}

	return "unknown"
}

func (b *Builder) detectNodePackageManager(dir string) string {
	if _, err := os.Stat(filepath.Join(dir, "pnpm-lock.yaml")); !os.IsNotExist(err) {
		return "pnpm"
	}

	if _, err := os.Stat(filepath.Join(dir, "package-lock.json")); !os.IsNotExist(err) {
		return "npm"
	}

	if _, err := os.Stat(filepath.Join(dir, "yarn.lock")); !os.IsNotExist(err) {
		return "yarn"
	}

	return "unknown"
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8083"
	}

	storageURL := os.Getenv("STORAGE_URL")
	if storageURL == "" {
		storageURL = "http://storage:8084"
	}

	log.Println("🚀 Starting Builder Service...")

	workDir := "/app/work"
	err := os.MkdirAll(workDir, 0755)
	if err != nil {
		log.Fatalf("❌ Failed to create work directory: %v", err)
	}

	kafkaProducer, err := kafka.NewProducer("kafka:29092")
	if err != nil {
		log.Fatalf("❌ Failed to create Kafka producer: %v", err)
	}
	defer kafkaProducer.Close()
	log.Println("✅ Kafka producer created")

	kafkaConsumer, err := kafka.NewConsumer("kafka:29092", "builder")
	if err != nil {
		log.Fatalf("❌ Failed to create Kafka consumer: %v", err)
	}
	defer kafkaConsumer.Close()
	log.Println("✅ Kafka consumer created")

	// Subscribe to build-jobs topic (not build-requests to avoid loop)
	err = kafkaConsumer.Subscribe([]string{"build-jobs"})
	if err != nil {
		log.Fatalf("❌ Failed to subscribe to topics: %v", err)
	}
	log.Println("✅ Subscribed to build-jobs topic")

	redisClient := redis.NewClient(&redis.Options{
		Addr: "redis:6379",
	})
	defer redisClient.Close()
	log.Println("✅ Redis client created")

	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown"
	}
	builder := NewBuilder(fmt.Sprintf("builder-%s", hostname), workDir, storageURL, kafkaProducer, redisClient)

	go func() {
		log.Println("🎧 Starting to consume messages from build-jobs...")
		kafkaConsumer.ConsumeMessages(func(key, value []byte) error {
			log.Printf("📨 Received message with key: %s", string(key))
			log.Printf("📨 Message content: %s", string(value))

			var buildReq message.BuildRequestMessage
			if err := kafka.UnmarshalMessage(value, &buildReq); err != nil {
				log.Printf("❌ Failed to unmarshal build request: %v", err)
				return err
			}

			log.Printf("🔨 Processing build request: %s", buildReq.ID)
			return builder.ProcessBuildJob(buildReq)
		})
	}()

	r := mux.NewRouter()

	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	log.Printf("🌐 Builder Service is running on port %s...", port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}
