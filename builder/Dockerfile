FROM gobuild-dependencies:latest AS builder
WORKDIR /app/builder
COPY builder/ .
RUN go mod edit -replace gobuild/shared=/app/shared
RUN go mod tidy
RUN go build -o builder .

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y librdkafka1 ca-certificates git nodejs npm golang && rm -rf /var/lib/apt/lists/*

RUN npm install -g pnpm yarn

WORKDIR /app
COPY --from=builder /app/builder/builder .
EXPOSE 8083
CMD ["./builder"]