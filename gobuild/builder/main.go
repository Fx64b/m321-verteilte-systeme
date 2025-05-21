package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/gorilla/mux"
	"gobuild/shared/kafka"
	"gobuild/shared/message"
)

// Builder executes build jobs
type Builder struct {
	id            string
	workDir       string
	kafkaProducer *kafka.Producer
}

// NewBuilder creates a new Builder
func NewBuilder(id, workDir string, kafkaProducer *kafka.Producer) *Builder {
	return &Builder{
		id:            id,
		workDir:       workDir,
		kafkaProducer: kafkaProducer,
	}
}

// ProcessBuildJob processes a build job
func (b *Builder) ProcessBuildJob(buildReq message.BuildRequestMessage) error {
	// Send status update: in-progress
	statusMsg := message.BuildStatusMessage{
		BuildID:   buildReq.ID,
		Status:    "in-progress",
		Message:   "Build started",
		UpdatedAt: time.Now(),
	}
	err := b.kafkaProducer.SendMessage("build-status", buildReq.ID, statusMsg)
	if err != nil {
		return err
	}

	buildDir := filepath.Join(b.workDir, buildReq.ID)
	err = os.MkdirAll(buildDir, 0755)
	if err != nil {
		return b.failBuild(buildReq.ID, fmt.Sprintf("Failed to create build directory: %v", err))
	}

	logMsg := message.BuildLogMessage{
		BuildID:   buildReq.ID,
		LogEntry:  "Cloning repository...",
		Timestamp: time.Now(),
	}
	b.kafkaProducer.SendMessage("build-logs", buildReq.ID, logMsg)

	cloneCmd := exec.Command("git", "clone", buildReq.RepositoryURL, buildDir)
	output, err := cloneCmd.CombinedOutput()
	if err != nil {
		logMsg := message.BuildLogMessage{
			BuildID:   buildReq.ID,
			LogEntry:  fmt.Sprintf("Clone failed: %s\n%s", err.Error(), output),
			Timestamp: time.Now(),
		}
		b.kafkaProducer.SendMessage("build-logs", buildReq.ID, logMsg)
		return b.failBuild(buildReq.ID, "Failed to clone repository")
	}

	if buildReq.Branch != "" {
		logMsg := message.BuildLogMessage{
			BuildID:   buildReq.ID,
			LogEntry:  fmt.Sprintf("Checking out branch: %s", buildReq.Branch),
			Timestamp: time.Now(),
		}
		b.kafkaProducer.SendMessage("build-logs", buildReq.ID, logMsg)

		checkoutCmd := exec.Command("git", "checkout", buildReq.Branch)
		checkoutCmd.Dir = buildDir
		output, err := checkoutCmd.CombinedOutput()
		if err != nil {
			logMsg := message.BuildLogMessage{
				BuildID:   buildReq.ID,
				LogEntry:  fmt.Sprintf("Checkout failed: %s\n%s", err.Error(), output),
				Timestamp: time.Now(),
			}
			b.kafkaProducer.SendMessage("build-logs", buildReq.ID, logMsg)
			return b.failBuild(buildReq.ID, "Failed to checkout branch")
		}
	}

	buildFilePath := filepath.Join(buildDir, "build.sh")
	if _, err := os.Stat(buildFilePath); os.IsNotExist(err) {
		projectType := b.detectProjectType(buildDir)
		switch projectType {
		case "node":
			logMsg := message.BuildLogMessage{
				BuildID:   buildReq.ID,
				LogEntry:  "Detected Node.js project, running npm install and build",
				Timestamp: time.Now(),
			}
			b.kafkaProducer.SendMessage("build-logs", buildReq.ID, logMsg)

			npmInstallCmd := exec.Command("npm", "install")
			npmInstallCmd.Dir = buildDir
			output, err := npmInstallCmd.CombinedOutput()
			if err != nil {
				logMsg := message.BuildLogMessage{
					BuildID:   buildReq.ID,
					LogEntry:  fmt.Sprintf("npm install failed: %s\n%s", err.Error(), output),
					Timestamp: time.Now(),
				}
				b.kafkaProducer.SendMessage("build-logs", buildReq.ID, logMsg)
				return b.failBuild(buildReq.ID, "Failed to install dependencies")
			}

			npmBuildCmd := exec.Command("npm", "run", "build")
			npmBuildCmd.Dir = buildDir
			output, err = npmBuildCmd.CombinedOutput()
			if err != nil {
				logMsg := message.BuildLogMessage{
					BuildID:   buildReq.ID,
					LogEntry:  fmt.Sprintf("npm build failed: %s\n%s", err.Error(), output),
					Timestamp: time.Now(),
				}
				b.kafkaProducer.SendMessage("build-logs", buildReq.ID, logMsg)
				return b.failBuild(buildReq.ID, "Failed to build project")
			}
		case "go":
			logMsg := message.BuildLogMessage{
				BuildID:   buildReq.ID,
				LogEntry:  "Detected Go project, running go build",
				Timestamp: time.Now(),
			}
			b.kafkaProducer.SendMessage("build-logs", buildReq.ID, logMsg)

			goBuildCmd := exec.Command("go", "build", "-o", "app")
			goBuildCmd.Dir = buildDir
			output, err := goBuildCmd.CombinedOutput()
			if err != nil {
				logMsg := message.BuildLogMessage{
					BuildID:   buildReq.ID,
					LogEntry:  fmt.Sprintf("go build failed: %s\n%s", err.Error(), output),
					Timestamp: time.Now(),
				}
				b.kafkaProducer.SendMessage("build-logs", buildReq.ID, logMsg)
				return b.failBuild(buildReq.ID, "Failed to build project")
			}
		default:
			return b.failBuild(buildReq.ID, "Unknown project type, no build script found")
		}
	} else {
		logMsg := message.BuildLogMessage{
			BuildID:   buildReq.ID,
			LogEntry:  "Executing build script",
			Timestamp: time.Now(),
		}
		b.kafkaProducer.SendMessage("build-logs", buildReq.ID, logMsg)

		buildCmd := exec.Command("/bin/sh", "build.sh")
		buildCmd.Dir = buildDir
		output, err := buildCmd.CombinedOutput()
		if err != nil {
			logMsg := message.BuildLogMessage{
				BuildID:   buildReq.ID,
				LogEntry:  fmt.Sprintf("Build script failed: %s\n%s", err.Error(), output),
				Timestamp: time.Now(),
			}
			b.kafkaProducer.SendMessage("build-logs", buildReq.ID, logMsg)
			return b.failBuild(buildReq.ID, "Build script failed")
		}
	}

	logMsg = message.BuildLogMessage{
		BuildID:   buildReq.ID,
		LogEntry:  "Build completed successfully",
		Timestamp: time.Now(),
	}
	b.kafkaProducer.SendMessage("build-logs", buildReq.ID, logMsg)

	// Create a tarball of the build artifacts
	artifactPath := fmt.Sprintf("/app/artifacts/%s.tar.gz", buildReq.ID)
	tarCmd := exec.Command("tar", "-czf", artifactPath, ".")
	tarCmd.Dir = buildDir
	output, err = tarCmd.CombinedOutput()
	if err != nil {
		logMsg := message.BuildLogMessage{
			BuildID:   buildReq.ID,
			LogEntry:  fmt.Sprintf("Failed to create artifact: %s\n%s", err.Error(), output),
			Timestamp: time.Now(),
		}
		b.kafkaProducer.SendMessage("build-logs", buildReq.ID, logMsg)
		return b.failBuild(buildReq.ID, "Failed to create artifact")
	}

	completionMsg := message.BuildCompletionMessage{
		BuildID:     buildReq.ID,
		Status:      "success",
		ArtifactURL: fmt.Sprintf("/artifacts/%s.tar.gz", buildReq.ID),
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

// failBuild handles build failures
func (b *Builder) failBuild(buildID, errorMsg string) error {
	completionMsg := message.BuildCompletionMessage{
		BuildID:     buildID,
		Status:      "failure",
		ArtifactURL: "",
		Duration:    0, // We don't have the start time here
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

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8082"
	}

	workDir := "/app/work"
	err := os.MkdirAll(workDir, 0755)
	if err != nil {
		log.Fatalf("Failed to create work directory: %v", err)
	}

	kafkaProducer, err := kafka.NewProducer("kafka:29092")
	if err != nil {
		log.Fatalf("Failed to create Kafka producer: %v", err)
	}
	defer kafkaProducer.Close()

	kafkaConsumer, err := kafka.NewConsumer("kafka:29092", "builder")
	if err != nil {
		log.Fatalf("Failed to create Kafka consumer: %v", err)
	}
	defer kafkaConsumer.Close()

	err = kafkaConsumer.Subscribe([]string{"build-requests"})
	if err != nil {
		log.Fatalf("Failed to subscribe to topics: %v", err)
	}

	builder := NewBuilder(fmt.Sprintf("builder-%s", os.Getenv("HOSTNAME")), workDir, kafkaProducer)

	go func() {
		kafkaConsumer.ConsumeMessages(func(key, value []byte) error {
			var buildReq message.BuildRequestMessage
			if err := kafka.UnmarshalMessage(value, &buildReq); err != nil {
				return err
			}

			log.Printf("Processing build request: %s", buildReq.ID)
			return builder.ProcessBuildJob(buildReq)
		})
	}()

	r := mux.NewRouter()

	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	log.Printf("Builder Service is running on port %s...", port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}
