FROM gobuild-dependencies:latest AS builder
WORKDIR /app/storage
COPY storage/ .
RUN go mod edit -replace gobuild/shared=/app/shared
RUN go mod tidy
RUN go build -o storage .

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y librdkafka1 ca-certificates && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=builder /app/storage/storage .
EXPOSE 8084
CMD ["./storage"]