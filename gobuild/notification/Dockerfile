FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN go mod init notification && \
    go mod tidy && \
    go build -o notification .

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/notification .
EXPOSE 8084
CMD ["./notification"]