FROM golang:1.24.3
WORKDIR /app
RUN apt-get update && apt-get install -y gcc g++ make git bash ca-certificates librdkafka-dev pkg-config
COPY shared/ /app/shared/
WORKDIR /app/shared
RUN go mod tidy
RUN go get github.com/confluentinc/confluent-kafka-go/v2@v2.3.0