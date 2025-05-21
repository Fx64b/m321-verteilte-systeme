FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN go mod init api-gateway && \
    go mod tidy && \
    go build -o api-gateway .

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/api-gateway .
EXPOSE 8080
CMD ["./api-gateway"]