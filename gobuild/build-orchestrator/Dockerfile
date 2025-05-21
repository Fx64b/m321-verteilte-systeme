FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN go mod init build-orchestrator && \
    go mod tidy && \
    go build -o build-orchestrator .

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/build-orchestrator .
EXPOSE 8081
CMD ["./build-orchestrator"]