package main

import (
	"log"
	"net/http"
	"os"
	"sync"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"gobuild/shared/kafka"
	"gobuild/shared/message"
)

type WebSocketClient struct {
	conn     *websocket.Conn
	buildID  string
	clientID string
}

type NotificationService struct {
	clients      map[string]*WebSocketClient
	clientsMutex sync.RWMutex
	upgrader     websocket.Upgrader
}

func NewNotificationService() *NotificationService {
	return &NotificationService{
		clients: make(map[string]*WebSocketClient),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins for this example
			},
		},
	}
}

func (ns *NotificationService) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Upgrade the HTTP connection to a WebSocket connection
	conn, err := ns.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to upgrade connection: %v", err)
		return
	}

	buildID := r.URL.Query().Get("buildId")
	clientID := r.URL.Query().Get("clientId")

	if buildID == "" || clientID == "" {
		log.Printf("Missing buildId or clientId")
		conn.Close()
		return
	}

	client := &WebSocketClient{
		conn:     conn,
		buildID:  buildID,
		clientID: clientID,
	}

	ns.clientsMutex.Lock()
	ns.clients[clientID] = client
	ns.clientsMutex.Unlock()

	log.Printf("Client connected: %s for build %s", clientID, buildID)

	// Handle disconnection
	defer func() {
		ns.clientsMutex.Lock()
		delete(ns.clients, clientID)
		ns.clientsMutex.Unlock()
		conn.Close()
		log.Printf("Client disconnected: %s", clientID)
	}()

	// Keep the connection alive
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			break
		}
	}
}

// BroadcastBuildStatus broadcasts a build status update to all connected clients
func (ns *NotificationService) BroadcastBuildStatus(statusMsg message.BuildStatusMessage) {
	ns.clientsMutex.RLock()
	defer ns.clientsMutex.RUnlock()

	for _, client := range ns.clients {
		if client.buildID == statusMsg.BuildID || client.buildID == "" {
			client.conn.WriteJSON(map[string]interface{}{
				"type":    "status",
				"buildId": statusMsg.BuildID,
				"status":  statusMsg.Status,
				"message": statusMsg.Message,
				"time":    statusMsg.UpdatedAt,
			})
		}
	}
}

// BroadcastBuildLog broadcasts a build log entry to all connected clients
func (ns *NotificationService) BroadcastBuildLog(logMsg message.BuildLogMessage) {
	ns.clientsMutex.RLock()
	defer ns.clientsMutex.RUnlock()

	for _, client := range ns.clients {
		if client.buildID == logMsg.BuildID || client.buildID == "" {
			client.conn.WriteJSON(map[string]interface{}{
				"type":    "log",
				"buildId": logMsg.BuildID,
				"log":     logMsg.LogEntry,
				"time":    logMsg.Timestamp,
			})
		}
	}
}

func (ns *NotificationService) BroadcastBuildCompletion(completionMsg message.BuildCompletionMessage) {
	ns.clientsMutex.RLock()
	defer ns.clientsMutex.RUnlock()

	for _, client := range ns.clients {
		if client.buildID == completionMsg.BuildID || client.buildID == "" {
			client.conn.WriteJSON(map[string]interface{}{
				"type":        "completion",
				"buildId":     completionMsg.BuildID,
				"status":      completionMsg.Status,
				"artifactUrl": completionMsg.ArtifactURL,
				"duration":    completionMsg.Duration,
				"time":        completionMsg.CompletedAt,
			})
		}
	}
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8084"
	}

	kafkaConsumer, err := kafka.NewConsumer("kafka:29092", "notification")
	if err != nil {
		log.Fatalf("Failed to create Kafka consumer: %v", err)
	}
	defer kafkaConsumer.Close()

	// Subscribe to build event topics
	err = kafkaConsumer.Subscribe([]string{"build-status", "build-logs", "build-completions"})
	if err != nil {
		log.Fatalf("Failed to subscribe to topics: %v", err)
	}

	notificationService := NewNotificationService()

	go func() {
		kafkaConsumer.ConsumeMessages(func(key, value []byte) error {
			topic := string(key)

			switch topic {
			case "build-status":
				var statusMsg message.BuildStatusMessage
				if err := kafka.UnmarshalMessage(value, &statusMsg); err != nil {
					return err
				}
				notificationService.BroadcastBuildStatus(statusMsg)
			case "build-logs":
				var logMsg message.BuildLogMessage
				if err := kafka.UnmarshalMessage(value, &logMsg); err != nil {
					return err
				}
				notificationService.BroadcastBuildLog(logMsg)
			case "build-completions":
				var completionMsg message.BuildCompletionMessage
				if err := kafka.UnmarshalMessage(value, &completionMsg); err != nil {
					return err
				}
				notificationService.BroadcastBuildCompletion(completionMsg)
			}

			return nil
		})
	}()

	r := mux.NewRouter()

	r.HandleFunc("/ws", notificationService.HandleWebSocket)

	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	log.Printf("Notification Service is running on port %s...", port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}
