FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN go mod init status-dashboard-api && \
    go mod tidy && \
    go build -o status-dashboard-api .

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/status-dashboard-api .
EXPOSE 8085
CMD ["./status-dashboard-api"]