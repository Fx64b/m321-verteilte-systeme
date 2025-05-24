# GoBuild - Verteiltes Build-System

GoBuild ist ein modernes, verteiltes Build-System für das Builden von NodeJs basierten Applikationen. 
Das System wurde mit Go und React als Microservice-Architekturen entwickelt.


## Aufgabenstellung

### 1. Systemaufbau

Das System besteht aus 7 Mikroservices:

- **API Gateway** (Port 8081): Zentraler Eingangspunkt mit JWT-Authentifizierung
- **Build Orchestrator** (Port 8082): Verwaltet Build-Jobs und Statusverfolgung
- **Builder** (Port 8083): Führt Build-Prozesse aus (5 Replicas für Skalierung)
- **Storage** (Port 8084): Verwaltet Build-Artefakte und Downloads
- **Notification** (Port 8085): WebSocket-Service für Live-Updates
- **Status Dashboard API** (Port 8086): REST API für das Frontend
- **Status Dashboard UI** (Port 3000): React-Frontend mit shadcn/ui

**Kommunikation über REST, Kafka und WebSockets:**
- **REST APIs**: Alle Service-zu-Service HTTP-Kommunikation, zum Beispiel:
  - Status Dashboard → API Gateway
  - Builder → Storage
- **Apache Kafka**: Asynchrones Messaging zwischen Services
- **WebSockets**: Live-Updates für Build-Status und Logs

**Sinnvoll gekapselt und konsistent:**
- Jeder Service hat eine klar definierte Verantwortlichkeit
- Konsistente Go-Module-Struktur mit geteilten Bibliotheken
- Einheitliche Fehlerbehandlung und Logging

### 2. Authentifizierung

**JWT-Implementierung:**
- Vollständige JWT-Authentifizierung mit HS256-Signierung
- User Registration und Login über REST API
- Token-basierte Authorisierung für alle geschützten Endpunkte
- Middleware-basierte Token-Validierung im API Gateway

**Sicherheitsfeatures:**
- Passwort-Hashing mit bcrypt
- Token-Expiration (24 Stunden)
- Schutz aller Build-Endpunkte vor unbefugtem Zugriff

### 3. Datenhaltung & Zustand

**Persistente Datenhaltung:**
- **Redis**: Zentrale Datenhaltung für Benutzer, Build-Status und Logs
- **Dateisystem**: Persistente Speicherung von Build-Artefakten
- **Kafka Topics**: Message-Speicherung und Queueing

**Stateless vs. Stateful:**
- **Stateless**: API Gateway, Builder-Services (horizontal skalierbar)
- **Stateful**: Redis (Datenbank), Storage (Dateisystem)
- Klare Trennung ermöglicht unabhängige Skalierung

### 4. Bereitstellung via Docker

**Vollständige Docker-Compose-Konfiguration:**
- Alle Services mit individuellen Dockerfiles
- Abhängigkeitsmanagement mit `depends_on` und `healthcheck`
- Multi-Stage Builds für optimierte Images
- Persistente Volumes für Daten und Artefakte

**Infrastruktur-Services:**
- Apache Kafka mit Zookeeper
- Redis mit Persistierung
- Automatische Kafka-Topic-Erstellung bei Start


## Schnellstart

### Voraussetzungen

- Docker und Docker Compose V2
- Git
- Ports 3000, 8081-8086, 9092, 6379 verfügbar

### Installation & Start

1. **Repository klonen:**
```bash
git clone https://github.com/Fx64b/m321-verteilte-systeme
cd m321-verteilte-systeme
```

2. **System starten:**
```bash
./build.sh
```

Oder manuell mit:
```bash
# Build Dependencies
docker build -t gobuild-dependencies:latest -f dependencies.Dockerfile .

docker-compose up --build
```

> **Hinweis:** Das Starten des Systems kann einige Minuten in Anspruch nehmen, da alle Images heruntergeladen und die Container gestartet werden müssen.

4. **Zugriff auf das Dashboard:**
Browser öffnen und auf [http://localhost:3000](http://localhost:3000) navigieren.


### System testen
**Schritt 1: Account erstellen**

1. Öffnen Sie http://localhost:3000
2. Klicken Sie auf "Sign In" → "Need an account?"
3. Registrieren Sie sich mit E-Mail und Passwort

**Schritt 2: Build starten**

1. Wechseln Sie zum Tab "New Build"
2. Geben Sie eine Repository-URL ein, z.B.:
```txt
https://github.com/Fx64b/fx64b.dev
```
3. Klicken Sie auf "Submit Build"

**Schritt 3: Build-Status prüfen**
1. Man wird automatisch zur Build-Status-Seite weitergeleitet
2. Logs werden in Echtzeit angezeigt
3. Nach erfolgreichem Build können Artefakte heruntergeladen werden

### Erweiterte Features

#### Throughput Testing
- URL: http://localhost:3000/throughput
- Gleichzeitige Ausführung mehrerer Builds
- Performance-Metriken und Statistiken

Für das Testing können folgende URLs verwendet werden:
```txt
https://github.com/Fx64b/fx64b.dev
https://github.com/jenkins-docs/simple-node-js-react-npm-app
https://github.com/kriasoft/react-starter-kit
https://github.com/shinokada/svelte-starter
https://github.com/tbreuss/mithril-by-examples
```
Diese können bei "Bulk Import" eingefügt werden.

> **Hinweis:** Je nach Performance des Systems kann es zwischen 1-3 Minuten dauern, bis alle Builds abgeschlossen sind.

#### Kafka Monitoring
Optional können Kafka Topics mit dem Tool [Redpanda](https://www.redpanda.com/) inspiziert und überwacht werden.
```bash
docker run --network=host \
  -e KAFKA_BROKERS=localhost:9092 \
  docker.redpanda.com/redpandadata/console:latest
```

Das Redpanda-Dashboard ist unter [http://localhost:8080](http://localhost:8080) erreichbar.

> **Hinweis:** Es wird vorausgesetzt, dass das `./build.sh` Script ausgeführt oder Docker-Compose manuell gestartet wurde, damit die Kafka-Umgebung korrekt konfiguriert ist.

## Architektur
![Systemarchitektur](docs/diagram.png)

**Datenfluss (vereinfacht)**

1. Benutzer-Authentifizierung: JWT-Token über API Gateway
2. Build-Anfrage: REST API → Kafka Message
3. Build-Verarbeitung: Orchestrator → Builder (parallel)
4. Status-Updates: Kafka → Notification → WebSocket → UI
5. Artefakt-Speicherung: Builder → Storage → Download-URL

**Technologien**

- Backend: Go 1.24, Gorilla Mux, JWT, bcrypt
- Frontend: React 19, Next.js 15, TypeScript, shadcn/ui
- Messaging: Apache Kafka
- Datenbank: Redis (Persistence + Caching)
- Containerisierung: Docker, Docker Compose
- Build-Tools: Git, Node.js, Go, tar/gzip