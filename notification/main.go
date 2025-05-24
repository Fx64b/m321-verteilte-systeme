package main

import (
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"gobuild/shared/kafka"
	"gobuild/shared/message"
	"log"
	"net/http"
	"os"
	"sync"
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

	if clientID == "" {
		log.Printf("Missing clientId")
		conn.Close()
		return
	}

	client := &WebSocketClient{
		conn:     conn,
		buildID:  buildID, // Can be empty to receive all build updates
		clientID: clientID,
	}

	ns.clientsMutex.Lock()
	ns.clients[clientID] = client
	ns.clientsMutex.Unlock()

	defer func() {
		ns.clientsMutex.Lock()
		delete(ns.clients, clientID)
		ns.clientsMutex.Unlock()
		conn.Close()
	}()

	// Keep the connection alive and handle ping/pong
	for {
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}

		// Handle ping messages
		if messageType == websocket.PingMessage {
			conn.WriteMessage(websocket.PongMessage, message)
		}
	}
}

// BroadcastBuildStatus broadcasts a build status update to all connected clients
func (ns *NotificationService) BroadcastBuildStatus(statusMsg message.BuildStatusMessage) {
	ns.clientsMutex.RLock()
	defer ns.clientsMutex.RUnlock()

	message := map[string]interface{}{
		"type":    "status",
		"buildId": statusMsg.BuildID,
		"status":  statusMsg.Status,
		"message": statusMsg.Message,
		"time":    statusMsg.UpdatedAt,
	}

	for clientID, client := range ns.clients {
		// Send to clients that are interested in this build or all builds
		if client.buildID == "" || client.buildID == statusMsg.BuildID {
			err := client.conn.WriteJSON(message)
			if err != nil {
				log.Printf("Failed to send message to client %s: %v", clientID, err)
				// Client will be cleaned up by the connection handler
			}
		}
	}

}

// BroadcastBuildLog broadcasts a build log entry to all connected clients
func (ns *NotificationService) BroadcastBuildLog(logMsg message.BuildLogMessage) {
	ns.clientsMutex.RLock()
	defer ns.clientsMutex.RUnlock()

	logMessage := map[string]interface{}{
		"type":    "log",
		"buildId": logMsg.BuildID,
		"log":     logMsg.LogEntry,
		"time":    logMsg.Timestamp,
	}

	for clientID, client := range ns.clients {
		// Send to clients that are interested in this build or all builds
		if client.buildID == "" || client.buildID == logMsg.BuildID {
			err := client.conn.WriteJSON(logMessage)
			if err != nil {
				log.Printf("Failed to send logMessage to client %s: %v", clientID, err)
				// Client will be cleaned up by the connection handler
			}
		}
	}
}

func (ns *NotificationService) BroadcastBuildCompletion(completionMsg message.BuildCompletionMessage) {
	ns.clientsMutex.RLock()
	defer ns.clientsMutex.RUnlock()

	buildMessage := map[string]interface{}{
		"type":        "completion",
		"buildId":     completionMsg.BuildID,
		"status":      completionMsg.Status,
		"artifactUrl": completionMsg.ArtifactURL,
		"duration":    completionMsg.Duration,
		"time":        completionMsg.CompletedAt,
	}

	for clientID, client := range ns.clients {
		// Send to clients that are interested in this build or all builds
		if client.buildID == "" || client.buildID == completionMsg.BuildID {
			err := client.conn.WriteJSON(buildMessage)
			if err != nil {
				log.Printf("Failed to send buildMessage to client %s: %v", clientID, err)
				// Client will be cleaned up by the connection handler
			}
		}
	}

	log.Printf("Broadcasted completion for build %s to %d clients", completionMsg.BuildID, len(ns.clients))
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8085"
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
			// Try to unmarshal as different message types
			var statusMsg message.BuildStatusMessage
			if err := kafka.UnmarshalMessage(value, &statusMsg); err == nil && statusMsg.BuildID != "" && statusMsg.Status != "" {
				if !statusMsg.UpdatedAt.IsZero() {
					notificationService.BroadcastBuildStatus(statusMsg)
				}
				return nil
			}

			var logMsg message.BuildLogMessage
			if err := kafka.UnmarshalMessage(value, &logMsg); err == nil && logMsg.BuildID != "" && logMsg.LogEntry != "" {
				notificationService.BroadcastBuildLog(logMsg)
				return nil
			}

			var completionMsg message.BuildCompletionMessage
			if err := kafka.UnmarshalMessage(value, &completionMsg); err == nil && completionMsg.BuildID != "" && completionMsg.Status != "" {
				log.Printf("✅ Received completion message for build: %s - Status: %s - ArtifactURL: %s",
					completionMsg.BuildID, completionMsg.Status, completionMsg.ArtifactURL)
				notificationService.BroadcastBuildCompletion(completionMsg)
				return nil
			}

			log.Printf("Unknown or invalid message type received")
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
