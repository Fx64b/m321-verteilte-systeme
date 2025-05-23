FROM gobuild-dependencies:latest AS builder
WORKDIR /app/build-orchestrator
COPY build-orchestrator/ .
RUN go mod edit -replace gobuild/shared=/app/shared
RUN go mod tidy
RUN go build -o build-orchestrator .

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y librdkafka1 ca-certificates && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=builder /app/build-orchestrator/build-orchestrator .
EXPOSE 8082
CMD ["./build-orchestrator"]