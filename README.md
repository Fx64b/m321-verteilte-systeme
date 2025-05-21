# m321-verteilte-systeme


## System Overview

GoBuild is a Vercel-like build pipeline system optimized for throughput, speed, and concurrency. Built entirely with Go and Docker Compose, it leverages Kafka for asynchronous communication, enabling high performance and scalability. The system provides a complete workflow from code submission to deployment with real-time monitoring.

## Architecture Components

The system consists of six microservices working together:

1. **API Gateway Service**
    - Entry point for all client requests
    - Request validation and routing
    - Authentication and authorization handling
    - Rate limiting and request queueing

2. **Build Orchestrator Service**
    - Build job creation and scheduling
    - Resource allocation for builds
    - Dependency resolution
    - Caching strategy implementation
    - Build prioritization logic

3. **Builder Service**
    - Isolated build execution environments
    - Parallel build processing
    - Build step execution
    - Code checkout and dependency installation
    - Compilation and testing

4. **Storage Service**
    - Build artifact management
    - Log persistence
    - Cache storage for dependencies and build outputs
    - Cleanup of older artifacts based on retention policies

5. **Notification Service**
    - Real-time status update distribution
    - Log processing and formatting
    - Event aggregation and filtering
    - Alert generation for failed builds

6. **Status Dashboard Service**
    - Real-time build monitoring UI
    - Live log streaming via WebSockets
    - Build history and analytics
    - Deployment status visualization
    - Error reporting interface

## Communication & Data Flow

The system employs a hybrid communication approach:

### Synchronous Communication (REST)
- Client requests to API Gateway
- Direct queries between services requiring immediate responses
- User authentication flows
- Dashboard data requests

### Asynchronous Communication (Kafka)
- **Kafka Topics:**
    - `build-requests`: New build jobs queue
    - `build-status`: Real-time build state changes
    - `build-logs`: Streaming console output
    - `build-completions`: Finished build results
    - `deployment-status`: Deployment state updates

### Key Event Flows

1. **Build Submission Flow:**
    - User submits repository URL to API Gateway
    - API Gateway validates request and forwards to Orchestrator
    - Orchestrator creates build job and publishes to `build-requests`
    - Available Builder instances consume build requests
    - Build progress events flow through `build-status` and `build-logs`
    - Completed builds published to `build-completions`

2. **Live Monitoring Flow:**
    - User connects to Status Dashboard
    - Dashboard establishes WebSocket connection
    - Dashboard service subscribes to relevant Kafka topics
    - Build events and logs streamed to connected clients in real-time

## Authentication

The system implements a robust authentication system:

- **JWT-based Authentication**
    - Tokens issued upon user login
    - User identity and permissions encoded in claims
    - Configurable token expiration
    - Refresh token mechanism for extended sessions

- **Alternative Access Methods**
    - API key authentication for headless/CI integration
    - Basic Auth headers for simple tool integration
    - OAuth integration for enterprise environments (optional)

## Data Storage & State Management

The system employs multiple storage strategies optimized for different data types:

- **Redis**
    - Build configuration caching
    - Build status information
    - Temporary credential storage
    - Rate limiting counters

- **In-memory State**
    - Active build tracking
    - WebSocket connection management
    - Request processing queues

- **Persistent Storage**
    - JSON files for configuration
    - Log files for build outputs
    - Artifact storage for build results

### State Classification

Services are designed with clear state boundaries:

- **Stateless Services**
    - API Gateway
    - Notification Service

- **Stateful Services**
    - Build Orchestrator
    - Storage Service
    - Status Dashboard (for active connections)
    - Builder (during active builds)

## Docker Deployment Architecture

The entire system is containerized using Docker Compose for easy local deployment:

- Each service runs in its own container
- Multiple Builder instances run in parallel for concurrent builds
- Kafka and Zookeeper containers for messaging
- Redis container for caching
- Persistent volume for build artifacts and logs
- Exposed ports for API Gateway and Status Dashboard
- Health checks for service dependency management
- Scale configuration for high-concurrency components

## User Experience Workflows

### Build and Deploy Workflow

1. User authenticates via API Gateway
2. User submits code repository for building
3. System queues build and returns a tracking ID
4. Build is assigned to available Builder
5. Builder clones repository and executes build pipeline
6. Build artifacts are stored upon completion
7. Deployment is triggered automatically if build succeeds
8. User receives notifications at key pipeline stages

### Monitoring Workflow

1. User navigates to Status Dashboard
2. Dashboard displays all builds associated with user
3. User selects specific build to monitor
4. Dashboard shows current build status with:
    - Real-time console output via WebSockets
    - Visual build progress indicators
    - Error highlighting for issues
    - Resource utilization metrics
5. Post-build, user can:
    - Review complete logs
    - Access build artifacts
    - Trigger redeployment if needed
    - Share build results with team members

## Performance Optimization Strategies

- **Build Concurrency**
    - Multiple Builder instances process jobs simultaneously
    - Intelligent load balancing based on Builder capacity
    - Resource-aware job allocation

- **Caching Mechanisms**
    - Dependency caching to reduce download time
    - Layer caching for faster container builds
    - Incremental builds when applicable

- **Stream Processing**
    - Real-time log streaming without buffering entire logs
    - Event-driven architecture to minimize polling
    - Selective update broadcasting to reduce network load

