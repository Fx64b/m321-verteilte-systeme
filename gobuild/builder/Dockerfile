FROM gobuild-dependencies:latest AS builder
WORKDIR /app/builder
COPY builder/ .
RUN go mod edit -replace gobuild/shared=/app/shared
RUN go mod tidy
RUN go build -o builder .

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y librdkafka1 ca-certificates git && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=builder /app/builder/builder .
EXPOSE 8082
CMD ["./builder"]