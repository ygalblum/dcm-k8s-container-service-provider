# Kubernetes Container Service Provider

A [DCM](https://github.com/dcm-project) (Data Center Management) service provider that manages containers in Kubernetes clusters. It exposes a REST API that maps container lifecycle operations to Kubernetes Deployments, Pods, and Services.

## Features

- **Container lifecycle management** — create, read, list, and delete containers via a REST API
- **Kubernetes-native** — each container maps to a Kubernetes Deployment; ports are exposed via Services (NodePort or LoadBalancer)
- **Resource constraints** — CPU (cores) and memory (MB/GB/TB) with min/max boundaries
- **Service exposure** — container ports can be `internal` (ClusterIP), `external` (NodePort or LoadBalancer), or `none`
- **Status monitoring** — watches Kubernetes resources and publishes status change events (PENDING, RUNNING, FAILED, UNKNOWN, DELETED) via NATS using [CloudEvents](https://cloudevents.io/) v1.0
- **Auto-registration** — registers itself with the DCM control plane on startup, with exponential backoff retry (1s initial, 60s max)
- **AEP-compliant API** — follows [API Enhancement Proposals](https://aep.dev/) standards; request validation is enforced via embedded OpenAPI spec
- **RFC 7807 errors** — all error responses use the Problem Details format

## Running with DCM

The recommended way to run this service provider alongside the full DCM stack (Service Provider Manager, Catalog Manager, NATS, PostgreSQL, etc.) is through the [DCM API Gateway](https://github.com/dcm-project/api-gateway).

The API Gateway repository contains a `compose.yaml` that orchestrates all DCM components. To include this service provider, use the `k8s-container` profile:

```bash
# Clone the API Gateway
git clone https://github.com/dcm-project/api-gateway.git
cd api-gateway

# Set the path to your kubeconfig
export K8S_CONTAINER_SP_KUBECONFIG="/path/to/kubeconfig"

# Start the full stack with the k8s container provider
podman-compose --profile k8s-container up -d
```

The gateway exposes all DCM APIs on port **9080**. See the [API Gateway README](https://github.com/dcm-project/api-gateway#readme) and its `RUN.md` for detailed instructions, including Kind cluster setup.

## Prerequisites

When running **outside** the API Gateway compose setup:

- **Kubernetes cluster** — accessible via kubeconfig file or in-cluster service account
- **NATS server** — for publishing container status events
- **DCM Service Provider Manager** — for provider registration (reachable at the URL set in `DCM_REGISTRATION_URL`)

## Configuration

All configuration is via environment variables. The service will fail to start if any required variable is missing or invalid.

### Provider Identity

| Variable | Required | Default | Description |
|---|---|---|---|
| `SP_NAME` | **Yes** | — | Provider name. Used as the registration identifier and CloudEvents source. |
| `SP_ENDPOINT` | **Yes** | — | Base URL of this service provider (e.g., `http://k8s-container-service-provider:8080`). The API path `/api/v1alpha1/containers` is appended automatically during registration. |
| `SP_DISPLAY_NAME` | No | *(empty)* | Human-readable display name. Included in registration payload when set. |
| `SP_REGION` | No | *(empty)* | Region code. Included in registration metadata when set. |
| `SP_ZONE` | No | *(empty)* | Availability zone. Included in registration metadata when set. |

### DCM Registry

| Variable | Required | Default | Description |
|---|---|---|---|
| `DCM_REGISTRATION_URL` | **Yes** | — | URL of the DCM Service Provider Manager (e.g., `http://service-provider-manager:8080/api/v1alpha1`). |

### Kubernetes

| Variable | Required | Default | Description |
|---|---|---|---|
| `SP_K8S_NAMESPACE` | No | `default` | Kubernetes namespace where Deployments and Services are created. |
| `SP_K8S_KUBECONFIG` | No | *(empty)* | Path to a kubeconfig file. When empty, in-cluster configuration is used. |
| `SP_K8S_EXTERNAL_SVC_TYPE` | **Yes** | — | Kubernetes Service type for externally-visible ports. Must be `LoadBalancer` or `NodePort`. |

### NATS

| Variable | Required | Default | Description |
|---|---|---|---|
| `SP_NATS_URL` | **Yes** | — | NATS server URL for publishing status events (e.g., `nats://nats:4222`). |

### Status Monitoring

| Variable | Required | Default | Description |
|---|---|---|---|
| `SP_MONITOR_DEBOUNCE_MS` | No | `500` | Debounce interval in milliseconds before publishing a status change event. |
| `SP_MONITOR_RESYNC_PERIOD` | No | `10m` | How often the monitor performs a full resync of Kubernetes resources. |

### HTTP Server

| Variable | Required | Default | Description |
|---|---|---|---|
| `SP_SERVER_ADDRESS` | No | `:8080` | TCP listen address (`host:port`). |
| `SP_SERVER_SHUTDOWN_TIMEOUT` | No | `15s` | Graceful shutdown timeout. |
| `SP_SERVER_READ_TIMEOUT` | No | `15s` | HTTP read timeout. |
| `SP_SERVER_WRITE_TIMEOUT` | No | `15s` | HTTP write timeout. |
| `SP_SERVER_IDLE_TIMEOUT` | No | `60s` | Idle connection timeout. |
| `SP_SERVER_REQUEST_TIMEOUT` | No | `30s` | Per-request timeout (applied via middleware). |

## API Endpoints

Base path: `/api/v1alpha1/containers`

### Health

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1alpha1/containers/health` | Returns health status, uptime (seconds), and version. |

Response example:

```json
{
  "type": "k8s-container-service-provider.dcm.io/health",
  "status": "healthy",
  "path": "health",
  "version": "0.0.1-dev",
  "uptime": 3600
}
```

### Container Operations

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1alpha1/containers` | List containers. Supports `max_page_size` (1–1000, default 50) and `page_token` query parameters. |
| `POST` | `/api/v1alpha1/containers` | Create a container. Accepts optional `id` query parameter (AEP-122 format: `^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`). |
| `GET` | `/api/v1alpha1/containers/{container_id}` | Get a container by ID. |
| `DELETE` | `/api/v1alpha1/containers/{container_id}` | Delete a container. Returns `204 No Content`. |

All error responses use [RFC 7807](https://www.rfc-editor.org/rfc/rfc7807) Problem Details with types: `INVALID_ARGUMENT`, `NOT_FOUND`, `ALREADY_EXISTS`, `INTERNAL`.

## Development

### Prerequisites

- Go 1.25.5 or later
- Access to a Kubernetes cluster
- Make

### Build and Run

```bash
make build    # Build binary to bin/k8s-container-service-provider
make run      # Run via go run
```

### Testing

```bash
make test          # Run all tests (Ginkgo v2, race detector)
make test-cover    # Run tests with coverage report (coverprofile.out)

# Run a specific test by TC-ID
go run github.com/onsi/ginkgo/v2/ginkgo -r -v -focus "TC-U009" internal/handlers/container
```

### Linting and Checks

```bash
make lint     # Run golangci-lint
make check    # fmt + vet + lint + test (full validation)
```

### Code Generation

The API is defined in `api/v1alpha1/openapi.yaml`. All request/response types, server interfaces, and the HTTP client are generated from it:

```bash
make generate-api         # Regenerate all code from OpenAPI spec
make check-generate-api   # Verify generated code is up to date
make check-aep            # Validate OpenAPI spec against AEP standards
```

Generated files (do not edit manually):
- `api/v1alpha1/types.gen.go` — data models
- `api/v1alpha1/spec.gen.go` — embedded OpenAPI spec
- `internal/api/server/server.gen.go` — Chi router and strict server interface
- `pkg/client/client.gen.go` — HTTP client

## Architecture

```
main.go
  -> HTTP server (internal/apiserver/) with middleware chain:
       Recovery -> Request Logging -> Request Timeout -> OpenAPI Validation
  -> Container handler (internal/handlers/container/)
       implements StrictServerInterface — typed request/response, no manual HTTP parsing
  -> ContainerRepository interface (internal/store/repository.go)
       -> Kubernetes implementation (internal/kubernetes/)
            maps containers to Deployments, Pods, and Services
  -> Status monitor (internal/monitoring/)
       watches K8s resources, publishes CloudEvents via NATS
  -> Registrar (internal/registration/)
       registers with DCM Service Provider Manager on startup
```

### Releasing

Images are pushed to `quay.io/dcm-project/k8s-container-service-provider`.
See [Releasing](https://github.com/dcm-project/shared-workflows#release-flow)
in shared-workflows for the full release process, tag behavior, and version conventions.

## License

Apache License 2.0 — see the [LICENSE](LICENSE) file for details.
