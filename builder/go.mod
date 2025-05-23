module gobuild/builder

go 1.24.3

require (
	github.com/gorilla/mux v1.8.1
	gobuild/shared v0.0.0
)

require github.com/confluentinc/confluent-kafka-go v1.9.2 // indirect

replace gobuild/shared => ../shared
