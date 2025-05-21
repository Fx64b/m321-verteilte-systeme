FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN go mod init builder && \
    go mod tidy && \
    go build -o builder .

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/builder .
EXPOSE 8082
CMD ["./builder"]