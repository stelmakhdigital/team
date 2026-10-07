
# Собрать всё вместе — финальная структура проекта, deployment

## 1. Финальная структура проекта

```
daemon/
├── cmd/
│   ├── daemon/
│   │   └── main.go              # Точка входа daemon
│   ├── cli/
│   │   └── main.go              # CLI утилита
│   └── pi-config/
│       └── main.go              # Утилита для управления Pi конфигами
│
├── internal/
│   ├── config/
│   │   ├── config.go            # Загрузка конфигурации
│   │   ├── validator.go         # Валидация
│   │   └── types.go             # Типы конфигов
│   │
│   ├── database/
│   │   ├── db.go                # Подключение к PostgreSQL
│   │   ├── migrate.go           # Миграции
│   │   ├── transaction.go       # Транзакции
│   │   └── migrations/
│   │       ├── 001_create_teams.sql
│   │       ├── 002_create_segments.sql
│   │       ├── 003_create_roles.sql
│   │       ├── 004_create_queue_tasks.sql
│   │       ├── 005_create_history_status.sql
│   │       ├── 006_create_sessions.sql
│   │       ├── 007_create_messages.sql
│   │       ├── 008_create_projects_goals.sql
│   │       ├── 009_create_security.sql
│   │       └── 010_create_audit.sql
│   │
│   ├── models/
│   │   ├── team.go
│   │   ├── segment.go
│   │   ├── role.go
│   │   ├── task.go
│   │   ├── session.go
│   │   ├── message.go
│   │   ├── project.go
│   │   ├── goal.go
│   │   ├── user.go
│   │   └── audit.go
│   │
│   ├── repository/
│   │   ├── team_repo.go
│   │   ├── segment_repo.go
│   │   ├── role_repo.go
│   │   ├── task_repo.go
│   │   ├── session_repo.go
│   │   ├── message_repo.go
│   │   ├── project_repo.go
│   │   ├── goal_repo.go
│   │   ├── user_repo.go
│   │   ├── audit_repo.go
│   │   └── interfaces.go
│   │
│   ├── service/
│   │   ├── team_service.go
│   │   ├── task_service.go
│   │   ├── session_service.go
│   │   ├── message_service.go
│   │   ├── project_service.go
│   │   ├── config_service.go
│   │   └── interfaces.go
│   │
│   ├── api/
│   │   ├── http/
│   │   │   ├── server.go
│   │   │   ├── routes.go
│   │   │   ├── middleware/
│   │   │   │   ├── auth.go
│   │   │   │   ├── logging.go
│   │   │   │   ├── recovery.go
│   │   │   │   └── ratelimit.go
│   │   │   ├── handlers/
│   │   │   │   ├── team_handler.go
│   │   │   │   ├── task_handler.go
│   │   │   │   ├── session_handler.go
│   │   │   │   ├── message_handler.go
│   │   │   │   ├── config_handler.go
│   │   │   │   └── audit_handler.go
│   │   │   └── dto/
│   │   │       ├── requests.go
│   │   │       └── responses.go
│   │   ├── grpc/
│   │   │   ├── server.go
│   │   │   ├── proto/
│   │   │   │   └── daemon.proto
│   │   │   └── handlers/
│   │   └── mcp/
│   │       ├── server.go
│   │       └── tools.go
│   │
│   ├── session/
│   │   ├── manager.go
│   │   ├── runtime/
│   │   │   ├── adapter.go
│   │   │   ├── container.go
│   │   │   ├── process.go
│   │   │   ├── tmux.go
│   │   │   └── pi.go
│   │   └── types.go
│   │
│   ├── orchestrator/
│   │   ├── task_orchestrator.go
│   │   ├── decomposer.go
│   │   └── worker.go
│   │
│   ├── watchdog/
│   │   ├── scanner.go
│   │   ├── policies.go
│   │   ├── detector.go
│   │   └── intervener.go
│   │
│   ├── metrics/
│   │   ├── metrics.go
│   │   └── collector.go
│   │
│   ├── security/
│   │   ├── auth.go
│   │   ├── rbac.go
│   │   ├── audit.go
│   │   ├── secrets.go
│   │   └── isolation.go
│   │
│   ├── events/
│   │   ├── bus.go
│   │   ├── types.go
│   │   └── handlers.go
│   │
│   └── utils/
│       ├── logger.go
│       ├── errors.go
│       ├── time.go
│       └── health.go
│
├── agents/
│   ├── pi-go-backend/
│   │   ├── agent.yaml
│   │   ├── guidance/
│   │   │   ├── role.md
│   │   │   └── context.md
│   │   ├── skills/
│   │   │   ├── code-review/
│   │   │   ├── tdd/
│   │   │   └── go-style/
│   │   └── hooks/
│   │       ├── pre-commit.sh
│   │       └── run-tests.sh
│   └── pi-reviewer/
│       ├── agent.yaml
│       └── ...
│
├── configs/
│   ├── daemon.yaml
│   ├── prometheus.yml
│   ├── alerts.yml
│   └── alertmanager.yml
│
├── deployments/
│   ├── docker/
│   │   ├── Dockerfile
│   │   ├── docker-compose.yml
│   │   └── docker-compose.prod.yml
│   ├── kubernetes/
│   │   ├── namespace.yaml
│   │   ├── configmap.yaml
│   │   ├── secrets.yaml
│   │   ├── deployment.yaml
│   │   ├── service.yaml
│   │   ├── ingress.yaml
│   │   ├── hpa.yaml
│   │   └── monitoring/
│   │       ├── prometheus-deployment.yaml
│   │       ├── grafana-deployment.yaml
│   │       └── alertmanager-deployment.yaml
│   └── systemd/
│       └── daemon.service
│
├── scripts/
│   ├── migrate.sh
│   ├── backup.sh
│   ├── restore.sh
│   └── health-check.sh
│
├── tests/
│   ├── unit/
│   │   ├── repository/
│   │   ├── service/
│   │   └── api/
│   ├── integration/
│   │   ├── task_flow_test.go
│   │   ├── session_lifecycle_test.go
│   │   └── watchdog_test.go
│   └── e2e/
│       ├── full_workflow_test.go
│       └── security_test.go
│
├── docs/
│   ├── architecture.md
│   ├── api.md
│   ├── deployment.md
│   ├── security.md
│   └── monitoring.md
│
├── go.mod
├── go.sum
├── Makefile
├── README.md
└── .goreleaser.yml
```


______________________________________________________________________

## 2. Конфигурация daemon

**configs/daemon.yaml:**

```yaml
server:
  http:
    host: "0.0.0.0"
    port: 8080
  grpc:
    host: "0.0.0.0"
    port: 9090
  metrics:
    host: "0.0.0.0"
    port: 9091

database:
  host: "${DB_HOST}"
  port: 5432
  user: "${DB_USER}"
  password: "${DB_PASSWORD}"
  name: "${DB_NAME}"
  ssl_mode: "disable"
  max_open_conns: 25
  max_idle_conns: 5
  conn_max_lifetime: "5m"

session:
  default_runtime: "container"
  container:
    docker_socket: "/var/run/docker.sock"
    network: "daemon-network"
    image: "agent-runtime:latest"
    enable_gpu: false
  process:
    working_dir: "/tmp/daemon/sessions"
    runtime_binary: "/usr/bin/agent-runtime"
  tmux:
    tmux_binary: "tmux"
    socket_name: "daemon"
  pi:
    tmux_binary: "tmux"
    socket_name: "daemon"
    pi_binary: "pi"
    workspaces_dir: "/workspaces"
    configs_dir: "/tmp/daemon/pi-configs"
    default_model: "claude-3-7-sonnet"

orchestrator:
  poll_interval: "5s"
  max_concurrent_leads: 10
  task_timeout: "24h"

watchdog:
  enabled: true
  scan_interval: "30s"
  stale_threshold: "2h"
  blocked_threshold: "4h"
  idle_threshold: "1h"
  hot_reload:
    enabled: true
    watch_config_file: true
    watch_interval: "10s"
    validate_on_reload: true
    graceful_timeout: "30s"

security:
  rbac_enabled: true
  audit_enabled: true
  secrets_encryption_key: "${SECRETS_KEY}"
  session:
    drop_capabilities: true
    read_only_rootfs: true
    max_pids: 100
    max_open_files: 1024

logging:
  level: "info"
  format: "json"
  output: "stdout"

metrics:
  enabled: true
  include_go_metrics: true
  include_process_metrics: true

events:
  type: "memory"
  buffer_size: 1000

health:
  enabled: true
  port: 8081
```


______________________________________________________________________

## 3. Dockerfile

**deployments/docker/Dockerfile:**

```dockerfile
# Build stage
FROM golang:1.21-alpine AS builder

RUN apk add --no-cache git make

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build daemon
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s -X main.Version=${VERSION:-dev}" \
    -o /build/daemon ./cmd/daemon

# Runtime stage
FROM alpine:3.18

RUN apk add --no-cache \
    ca-certificates \
    tmux \
    docker-cli \
    curl \
    && update-ca-certificates

# Создать non-root пользователя
RUN addgroup -g 1000 daemon && \
    adduser -D -u 1000 -G daemon daemon

WORKDIR /app

# Копировать бинарник
COPY --from=builder /build/daemon /app/daemon

# Копировать конфиги
COPY configs/daemon.yaml /app/configs/daemon.yaml

# Копировать agents
COPY agents/ /app/agents/

# Директории для runtime
RUN mkdir -p /tmp/daemon/sessions \
    /tmp/daemon/pi-configs \
    /workspaces \
    && chown -R daemon:daemon /tmp/daemon /workspaces

USER daemon

EXPOSE 8080 8081 9090 9091

HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8081/health || exit 1

ENTRYPOINT ["/app/daemon"]
CMD ["--config", "/app/configs/daemon.yaml"]
```


______________________________________________________________________

## 4. Docker Compose

**deployments/docker/docker-compose.yml:**

```yaml
version: '3.8'

services:
  daemon:
    build:
      context: ../..
      dockerfile: deployments/docker/Dockerfile
    ports:
      - "8080:8080"  # API
      - "8081:8081"  # Health
      - "9091:9091"  # Metrics
    environment:
      - DB_HOST=db
      - DB_USER=daemon
      - DB_PASSWORD=${DB_PASSWORD}
      - DB_NAME=daemon
      - SECRETS_KEY=${SECRETS_KEY}
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - daemon_workspaces:/workspaces
      - daemon_configs:/tmp/daemon/pi-configs
    depends_on:
      db:
        condition: service_healthy
    restart: unless-stopped
    networks:
      - daemon-network
    deploy:
      resources:
        limits:
          cpus: '2.0'
          memory: 2G
        reservations:
          cpus: '1.0'
          memory: 1G

  db:
    image: postgres:15-alpine
    environment:
      - POSTGRES_USER=daemon
      - POSTGRES_PASSWORD=${DB_PASSWORD}
      - POSTGRES_DB=daemon
    volumes:
      - postgres_data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U daemon"]
      interval: 5s
      timeout: 5s
      retries: 5
    restart: unless-stopped
    networks:
      - daemon-network

  prometheus:
    image: prom/prometheus:v2.45.0
    volumes:
      - ../../configs/prometheus.yml:/etc/prometheus/prometheus.yml
      - ../../configs/alerts.yml:/etc/prometheus/alerts.yml
      - prometheus_data:/prometheus
    command:
      - '--config.file=/etc/prometheus/prometheus.yml'
      - '--storage.tsdb.path=/prometheus'
      - '--web.enable-lifecycle'
    ports:
      - "9090:9090"
    depends_on:
      - daemon
    restart: unless-stopped
    networks:
      - daemon-network

  grafana:
    image: grafana/grafana:10.0.0
    volumes:
      - grafana_data:/var/lib/grafana
      - ../../grafana/provisioning:/etc/grafana/provisioning
    environment:
      - GF_SECURITY_ADMIN_USER=${GRAFANA_USER}
      - GF_SECURITY_ADMIN_PASSWORD=${GRAFANA_PASSWORD}
      - GF_USERS_ALLOW_SIGN_UP=false
    ports:
      - "3000:3000"
    depends_on:
      - prometheus
    restart: unless-stopped
    networks:
      - daemon-network

  alertmanager:
    image: prom/alertmanager:v0.25.0
    volumes:
      - ../../configs/alertmanager.yml:/etc/alertmanager/alertmanager.yml
      - alertmanager_data:/alertmanager
    ports:
      - "9093:9093"
    depends_on:
      - prometheus
    restart: unless-stopped
    networks:
      - daemon-network

volumes:
  postgres_data:
  prometheus_data:
  grafana_data:
  alertmanager_data:
  daemon_workspaces:
  daemon_configs:

networks:
  daemon-network:
    driver: bridge
```


______________________________________________________________________

## 5. Kubernetes Deployment

**deployments/kubernetes/deployment.yaml:**

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: daemon
  namespace: daemon
  labels:
    app: daemon
spec:
  replicas: 1  # Stateful, поэтому 1 реплика
  selector:
    matchLabels:
      app: daemon
  template:
    metadata:
      labels:
        app: daemon
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/port: "9091"
    spec:
      serviceAccountName: daemon
      securityContext:
        runAsNonRoot: true
        runAsUser: 1000
        fsGroup: 1000
      containers:
        - name: daemon
          image: daemon:latest
          imagePullPolicy: Always
          ports:
            - name: http
              containerPort: 8080
            - name: health
              containerPort: 8081
            - name: metrics
              containerPort: 9091
          env:
            - name: DB_HOST
              valueFrom:
                secretKeyRef:
                  name: daemon-secrets
                  key: db-host
            - name: DB_USER
              valueFrom:
                secretKeyRef:
                  name: daemon-secrets
                  key: db-user
            - name: DB_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: daemon-secrets
                  key: db-password
            - name: DB_NAME
              value: "daemon"
            - name: SECRETS_KEY
              valueFrom:
                secretKeyRef:
                  name: daemon-secrets
                  key: secrets-key
          envFrom:
            - configMapRef:
                name: daemon-config
          volumeMounts:
            - name: docker-socket
              mountPath: /var/run/docker.sock
            - name: workspaces
              mountPath: /workspaces
            - name: configs
              mountPath: /tmp/daemon/pi-configs
          resources:
            requests:
              cpu: "1"
              memory: 1Gi
            limits:
              cpu: "2"
              memory: 2Gi
          livenessProbe:
            httpGet:
              path: /health
              port: health
            initialDelaySeconds: 10
            periodSeconds: 10
            timeoutSeconds: 5
            failureThreshold: 3
          readinessProbe:
            httpGet:
              path: /health
              port: health
            initialDelaySeconds: 5
            periodSeconds: 5
            timeoutSeconds: 3
            failureThreshold: 3
      volumes:
        - name: docker-socket
          hostPath:
            path: /var/run/docker.sock
        - name: workspaces
          persistentVolumeClaim:
            claimName: daemon-workspaces-pvc
        - name: configs
          emptyDir: {}
      affinity:
        podAntiAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
            - weight: 100
              podAffinityTerm:
                labelSelector:
                  matchLabels:
                    app: daemon
                topologyKey: kubernetes.io/hostname
```

**deployments/kubernetes/service.yaml:**

```yaml
apiVersion: v1
kind: Service
metadata:
  name: daemon
  namespace: daemon
spec:
  selector:
    app: daemon
  ports:
    - name: http
      port: 80
      targetPort: 8080
    - name: metrics
      port: 9091
      targetPort: 9091
  type: ClusterIP
```

**deployments/kubernetes/hpa.yaml:**

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: daemon
  namespace: daemon
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: daemon
  minReplicas: 1
  maxReplicas: 1  # Stateful, не масштабируется горизонтально
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 80
    - type: Resource
      resource:
        name: memory
        target:
          type: Utilization
          averageUtilization: 80
```


______________________________________________________________________

## 6. Makefile

**Makefile:**

```makefile
.PHONY: all build test clean run migrate docker-build docker-push deploy

VERSION ?= $(shell git describe --tags --always --dirty)
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
GIT_COMMIT ?= $(shell git rev-parse --short HEAD)

LDFLAGS := -w -s \
	-X main.Version=$(VERSION) \
	-X main.BuildTime=$(BUILD_TIME) \
	-X main.GitCommit=$(GIT_COMMIT)

all: build test

build:
	@echo "Building daemon..."
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o bin/daemon ./cmd/daemon
	@echo "Building CLI..."
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o bin/cli ./cmd/cli

test:
	@echo "Running tests..."
	go test -race -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

clean:
	rm -rf bin/ coverage.out

run: build
	./bin/daemon --config configs/daemon.yaml

migrate:
	@echo "Running database migrations..."
	go run ./cmd/daemon migrate up

docker-build:
	docker build \
		--build-arg VERSION=$(VERSION) \
		-t daemon:$(VERSION) \
		-f deployments/docker/Dockerfile .

docker-push:
	docker tag daemon:$(VERSION) registry.example.com/daemon:$(VERSION)
	docker push registry.example.com/daemon:$(VERSION)

deploy:
	kubectl apply -f deployments/kubernetes/

deploy-prod:
	kubectl apply -f deployments/kubernetes/ --namespace=daemon-prod

backup:
	@echo "Creating database backup..."
	./scripts/backup.sh

restore:
	@echo "Restoring database from backup..."
	./scripts/restore.sh $(BACKUP_FILE)

health-check:
	@echo "Running health checks..."
	./scripts/health-check.sh

lint:
	golangci-lint run

fmt:
	go fmt ./...
	goimports -w .

generate:
	go generate ./...

docs:
	@echo "Generating API documentation..."
	swag init -g cmd/daemon/main.go -o docs/api

release:
	goreleaser release --rm-dist

help:
	@echo "Available targets:"
	@echo "  build         - Build binaries"
	@echo "  test          - Run tests"
	@echo "  clean         - Clean build artifacts"
	@echo "  run           - Run daemon locally"
	@echo "  migrate       - Run database migrations"
	@echo "  docker-build  - Build Docker image"
	@echo "  docker-push   - Push Docker image"
	@echo "  deploy        - Deploy to Kubernetes"
	@echo "  backup        - Create database backup"
	@echo "  restore       - Restore database from backup"
	@echo "  lint          - Run linter"
	@echo "  fmt           - Format code"
	@echo "  release       - Create release"
```


______________________________________________________________________

## 7. Production Checklist

**Pre-deployment:**

- [ ] БД настроена и миграции применены
- [ ] Secrets созданы (DB_PASSWORD, SECRETS_KEY)
- [ ] Docker socket доступен (для container runtime)
- [ ] Сеть настроена (daemon-network)
- [ ] Monitoring stack развёрнут (Prometheus, Grafana)
- [ ] Alerts настроены
- [ ] Backup strategy определена
- [ ] Disaster recovery план

**Security:**

- [ ] RBAC включён
- [ ] Audit логирование включено
- [ ] Secrets зашифрованы
- [ ] Container security (non-root, capabilities dropped)
- [ ] Network policies настроены
- [ ] TLS для API (если нужно)

**Monitoring:**

- [ ] Prometheus scraping работает
- [ ] Grafana dashboards импортированы
- [ ] Alerts активны
- [ ] Health checks настроены

**Performance:**

- [ ] Resource limits установлены
- [ ] HPA настроен (если нужно)
- [ ] Database connection pool оптимизирован
- [ ] Caching включён (если нужно)

**Backup/Restore:**

- [ ] Backup скрипт протестирован
- [ ] Restore процедура протестирована
- [ ] Backup расписание настроено (cron)

______________________________________________________________________

## 8. Deployment Commands

**Local development:**

```bash
# Запустить всё через docker-compose
cd deployments/docker
docker-compose up -d

# Проверить статус
docker-compose ps

// Посмотреть логи
docker-compose logs -f daemon

// Остановить
docker-compose down
```

**Kubernetes:**

```bash
# Создать namespace
kubectl create namespace daemon

# Создать secrets
kubectl create secret generic daemon-secrets \
  --from-literal=db-password=xxx \
  --from-literal=secrets-key=yyy \
  -n daemon

# Применить конфигурацию
kubectl apply -f deployments/kubernetes/

# Проверить статус
kubectl get pods -n daemon
kubectl get services -n daemon

// Посмотреть логи
kubectl logs -f deployment/daemon -n daemon

// Port-forward для доступа
kubectl port-forward service/daemon 8080:80 -n daemon
```

**Production:**

```bash
# Build и push
make docker-build
make docker-push

# Deploy
make deploy-prod

# Check health
kubectl rollout status deployment/daemon -n daemon-prod

// Monitor
kubectl top pods -n daemon-prod
```


______________________________________________________________________

## 9. Post-Deployment

**Проверка:**

```bash
# Health check
curl http://localhost:8081/health

// API check
curl http://localhost:8080/api/v1/teams

// Metrics check
curl http://localhost:9091/metrics

// Prometheus targets
curl http://localhost:9090/api/v1/targets
```

**Grafana:**

- Open http://localhost:3000
- Login: admin / \${GRAFANA_PASSWORD}
- Import dashboards из deployments/grafana/dashboards/

______________________________________________________________________

## 10. Maintenance

**Ежедневные задачи:**

- Проверить dashboard'ы (errors, queue size, session health)
- Проверить alerts
- Проверить backup

**Еженедельные задачи:**

- Review audit logs
- Проверить resource usage
- Update dependencies

**Ежемесячные задачи:**

- Security audit
- Performance review
- Capacity planning
