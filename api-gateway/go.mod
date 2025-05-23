module gobuild/api-gateway

go 1.24.3

require (
	github.com/go-redis/redis/v8 v8.11.5
	github.com/golang-jwt/jwt/v5 v5.2.2
	github.com/google/uuid v1.3.0
	github.com/gorilla/mux v1.8.1
	gobuild/shared v0.0.0
	golang.org/x/crypto v0.35.0
)

require (
	github.com/cespare/xxhash/v2 v2.1.2 // indirect
	github.com/confluentinc/confluent-kafka-go/v2 v2.3.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
)

replace gobuild/shared => ../shared
