# GoBuild

GoBuild is a Vercel-like build pipeline system optimized for throughput, speed, and concurrency, built with Go and Docker Compose.

## Architecture

The system consists of six microservices:
- API Gateway: Entry point for client requests
- Build Orchestrator: Manages build jobs and scheduling
- Builder: Executes build processes
- Storage: Manages build artifacts and logs
- Notification: Handles status updates and alerts
- Status Dashboard: Provides UI for monitoring builds

## Technologies

- Backend: Go (Golang)
- Frontend: React with shadcn/ui components
- Message Queue: Apache Kafka
- Caching: Redis
- Containerization: Docker and Docker Compose

## Getting Started

### Prerequisites

- Docker and Docker Compose
- Git

### Setup and Run

1. Clone the repository:
```bash
git clone https://github.com/fx64b/gobuild.git
cd gobuild
./build.sh
```

Redpanda (For debugging kafka messages):
```bash
 docker run --network=host -e KAFKA_BROKERS=localhost:9092 -e SERVER_PORT=8089 docker.redpanda.com/redpandadata/console:latest
```

Then visit: [Redpanda Console](http://[::1]:8080/overview)