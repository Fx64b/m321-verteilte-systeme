FROM gobuild-dependencies:latest AS builder
WORKDIR /app/api-gateway
COPY api-gateway/ .
RUN go mod edit -replace gobuild/shared=/app/shared
RUN go mod tidy
RUN go build -o api-gateway .

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y librdkafka1 ca-certificates && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=builder /app/api-gateway/api-gateway .
EXPOSE 8081
CMD ["./api-gateway"]