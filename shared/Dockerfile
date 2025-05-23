FROM golang:1.24.3-alpine
RUN apk add --no-cache gcc g++ make git bash ca-certificates librdkafka-dev pkgconfig

WORKDIR /app/shared
COPY . .
RUN go mod tidy