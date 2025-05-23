FROM gobuild-dependencies:latest AS builder
WORKDIR /app/notification
COPY notification/ .
RUN go mod edit -replace gobuild/shared=/app/shared
RUN go mod tidy
RUN go build -o notification .

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y librdkafka1 ca-certificates && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=builder /app/notification/notification .
EXPOSE 8085
CMD ["./notification"]