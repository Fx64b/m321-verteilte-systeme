FROM gobuild-dependencies:latest AS builder
WORKDIR /app/build-orchestrator
COPY build-orchestrator/ .
RUN go mod edit -replace gobuild/shared=/app/shared
RUN go mod tidy
RUN go build -o build-orchestrator .

FROM alpine:latest
RUN apk add --no-cache librdkafka
WORKDIR /app
COPY --from=builder /app/build-orchestrator/build-orchestrator .
EXPOSE 8082
CMD ["./build-orchestrator"]