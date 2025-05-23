module gobuild/notification

go 1.24.3

require (
	github.com/gorilla/mux v1.8.1
	github.com/gorilla/websocket v1.5.3
	gobuild/shared v0.0.0
)

require github.com/confluentinc/confluent-kafka-go v1.9.2 // indirect

replace gobuild/shared => ../shared
