module gobuild/api-gateway

go 1.24.3

require (
	github.com/golang-jwt/jwt/v5 v5.2.2
	github.com/google/uuid v1.3.0
	github.com/gorilla/mux v1.8.1
	gobuild/shared v0.0.0
)

require github.com/confluentinc/confluent-kafka-go v1.9.2 // indirect

replace gobuild/shared => ../shared
