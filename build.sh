#!/bin/bash

CLEAR_FLAG=false
for arg in "$@"; do
  if [ "$arg" == "--clear" ]; then
    CLEAR_FLAG=true
    break
  fi
done

# clear flag to remove volumes as well
if [ "$CLEAR_FLAG" = true ]; then
  docker-compose down --volumes
else
  docker-compose down
fi

docker build -t gobuild-dependencies:latest -f dependencies.Dockerfile .

docker-compose up --build
