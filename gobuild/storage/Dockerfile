FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN go mod init storage && \
    go mod tidy && \
    go build -o storage .

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/storage .
EXPOSE 8083
CMD ["./storage"]