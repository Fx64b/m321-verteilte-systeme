#!/bin/bash

# Wait for Kafka to be ready
echo "Waiting for Kafka to be ready..."
until nc -z kafka 29092; do
  sleep 1
done
echo "Kafka is ready!"

TOPICS=(
  "build-requests"
  "build-status"
  "build-logs"
  "build-completions"
  "build-jobs"
)

for topic in "${TOPICS[@]}"; do
  echo "Creating topic: $topic"
  kafka-topics --create \
    --if-not-exists \
    --bootstrap-server kafka:29092 \
    --replication-factor 1 \
    --partitions 5 \
    --topic "$topic"
done

echo "All topics created successfully!"

exit 0