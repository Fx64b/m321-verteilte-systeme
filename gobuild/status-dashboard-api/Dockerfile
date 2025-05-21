FROM gobuild-dependencies:latest AS builder
WORKDIR /app/status-dashboard-api
COPY status-dashboard-api/ .
RUN go mod edit -replace gobuild/shared=/app/shared
RUN go mod tidy
RUN go build -o status-dashboard-api .

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y librdkafka1 ca-certificates && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=builder /app/status-dashboard-api/status-dashboard-api .
EXPOSE 8085
CMD ["./status-dashboard-api"]