#!/bin/bash

# Stop any running containers
docker-compose down

# Build the dependencies image first
docker build -t gobuild-dependencies:latest -f dependencies.Dockerfile .

# Build and start all services
docker-compose up --build