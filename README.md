# Simple Go Probe Service (healthz/readyz with delay + admin reset)

A minimal HTTP service intended for debugging orchestration behavior (e.g., Kubernetes liveness/readiness).
It exposes `/healthz` and `/readyz` which become **healthy/ready only after a configurable startup delay**.
Admin endpoints allow resetting probe state back to **unhealthy/not-ready** and re-applying the delay.

## Endpoints

### Probes
- `GET /healthz`
  - `200 OK` when health flag is `true`
  - `503 Service Unavailable` while health flag is `false`
- `GET /readyz`
  - `200 OK` when ready flag is `true`
  - `503 Service Unavailable` while ready flag is `false`

While not in the target state, the response includes `retry_after_ms` to indicate the remaining delay.

### Admin (state reset)
> **Security note:** These endpoints are intentionally unauthenticated. Do not expose them publicly.
> If you run behind a load balancer or in a cluster, protect them (network policy, auth, or bind to localhost).

- `POST /admin/reset`
  - Resets **both** health and ready to `false` and restarts the startup delay for both.
- `POST /admin/health/reset`
  - Resets **health** to `false` and restarts its delay.
- `POST /admin/ready/reset`
  - Resets **ready** to `false` and restarts its delay.

## Environment Variables

All configuration is done via environment variables.

| Variable | Default | Type | Description |
|---|---:|---|---|
| `PORT`           | `8080`            | int      | TCP port the server listens on. Valid range: `1..65535`. |
| `STARTUP_DELAY`  | `30s`             | duration | Delay applied to **both** `/healthz` and `/readyz` before they switch to the target state. |
| `SHUTDOWN_WAIT`  | `10s`             | duration | Graceful shutdown timeout. |
| `READ_TIMEOUT`   | `15s`             | duration | HTTP server read timeout. |
| `WRITE_TIMEOUT`  | `15s`             | duration | HTTP server write timeout. |
| `IDLE_TIMEOUT`   | `60s`             | duration | HTTP server idle timeout. |
| `MAX_BODY_BYTES` | `1048576` (1 MiB) | int64    | Maximum request body size enforced via `http.MaxBytesReader`. |
| `LOG_LEVEL`      | `info`            | string   | Log level: `debug`, `info`, `warn`, `error`. Logs are JSON (via `log/slog`). |

### Duration format
`STARTUP_DELAY`, `SHUTDOWN_WAIT`, `READ_TIMEOUT`, `WRITE_TIMEOUT`, `IDLE_TIMEOUT` use Go duration format, e.g.:
- `250ms`, `5s`, `1m`, `2h`

## Build & Run

### Build
```bash
make all
```

### Run with defaults
```bash
./bin/server
```

### Run with custom port and shorter delay
```bash
export PORT=9090
export STARTUP_DELAY=5s
./bin/server
```

### Run with debug logs
```bash
export LOG_LEVEL=debug
./bin/server
```

## Usage

### Examples (curl)

Assuming default port 8080.

#### 1) Observe startup delay behavior

##### Immediately after start:
```bash
curl -sS localhost:8080/healthz | jq
curl -sS localhost:8080/readyz  | jq
```

##### After the delay (default 30 seconds):
```bash
sleep 30
curl -sS localhost:8080/healthz | jq
curl -sS localhost:8080/readyz  | jq
```

#### 2) Reset both probes back to "not OK"

```bash
curl -sS -X POST localhost:8080/admin/reset | jq
curl -sS localhost:8080/healthz | jq
curl -sS localhost:8080/readyz  | jq
```

#### 3) Reset only readiness
```bash
curl -sS -X POST localhost:8080/admin/ready/reset | jq
curl -sS localhost:8080/readyz | jq
```

#### 4) Reset only health
```bash
curl -sS -X POST localhost:8080/admin/health/reset | jq
curl -sS localhost:8080/healthz | jq
```


### Run container (host port 8080 -> container port 8080)

```bash
docker run --rm -p 8080:8080 \
  -e PORT=8080 \
  -e STARTUP_DELAY=10s \
  -e LOG_LEVEL=info \
  probe-service:local
```

### Run container with different internal port
```bash
docker run --rm -p 9090:9090 \
  -e PORT=9090 \
  -e STARTUP_DELAY=3s \
  probe-service:local
```

### Example: Kubernetes Probes

Example container env and probes (adjust image/name as needed):

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: probe-service
spec:
  replicas: 1
  selector:
    matchLabels:
      app: probe-service
  template:
    metadata:
      labels:
        app: probe-service
    spec:
      containers:
        - name: probe-service
          image: your-registry/probe-service:latest
          env:
            - name: PORT
              value: "8080"
            - name: STARTUP_DELAY
              value: "30s"
            - name: LOG_LEVEL
              value: "info"
          ports:
            - containerPort: 8080
          readinessProbe:
            httpGet:
              path: /readyz
              port: 8080
            periodSeconds: 2
            failureThreshold: 3
          livenessProbe:
            httpGet:
              path: /healthz
              port: 8080
            periodSeconds: 5
            failureThreshold: 3
```
