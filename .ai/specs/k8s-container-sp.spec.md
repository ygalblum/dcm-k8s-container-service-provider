# Specification: K8s Container Service Provider

## 1. Overview

The Kubernetes Container Service Provider (K8s Container SP) is a REST API that
manages containerized workloads on Kubernetes clusters using Deployments. It
exposes endpoints for creating, reading, and deleting containers, integrates
with the DCM Service Provider Registry, reports resource status via CloudEvents
over NATS, and exposes a health endpoint for DCM control plane polling.

**Version scope (v1):**

- Create and delete container instances only (no update/day-2 operations)
- Single-container Deployments with replicas=1
- Kubernetes Deployments only (no Jobs, DaemonSets, StatefulSets, bare Pods)
- Ephemeral storage only (no persistent volumes)
- Single configured namespace for all managed resources

**Reference documents:**

- [K8s Container SP Enhancement](https://github.com/dcm-project/enhancements/blob/main/enhancements/k8s-container-sp/k8s-container-sp.md)
- [SP Registration Flow](https://github.com/dcm-project/enhancements/blob/main/enhancements/sp-registration-flow/sp-registration-flow.md)
- [SP Health Check](https://github.com/dcm-project/enhancements/blob/main/enhancements/service-provider-health-check/service-provider-health-check.md)
- [SP Status Reporting](https://github.com/dcm-project/enhancements/blob/main/enhancements/state-management/service-provider-status-reporting.md)
- [Service Type Definitions](https://github.com/dcm-project/enhancements/blob/main/enhancements/service-type-definitions/service-type-definitions.md)
- OpenAPI Spec: `api/v1alpha1/openapi.yaml` (source of truth for API contract)

---

## 2. Architecture

```
                                     +------------------+
                                     |   DCM Control    |
                                     |     Plane        |
                                     +--------+---------+
                                              |
                          +-------------------+-------------------+
                          ^                   |                   |
                          |                   |                   |
                   Registration         Health Poll         NATS Messages
                   POST /providers      GET /health         (CloudEvents)
                          |                   |                   |
                          |                   v                   |
+-------------------------+-------------------+-------------------+--------+
|                    K8s Container Service Provider                        |
|                                                                          |
|  +-------------+  +----------------+  +------------------+               |
|  | HTTP Server |--| API Handlers   |--| Container Store  |               |
|  | (chi)       |  | (endpoints)    |  | (interface)      |               |
|  +------+------+  +----------------+  +--------+---------+               |
|         |                                      |                         |
|  +------+------+                     +---------+---------+               |
|  | Health Svc  |                     | K8s Store (impl)  |               |
|  +-------------+                     +---------+---------+               |
|                                                |                         |
|  +-------------+                     +---------+---------+               |
|  | DCM Reg.    |                     | Status Monitor    |-----> NATS    |
|  | Client      |                     | (Informers)       |               |
|  +-------------+                     +-------------------+               |
+-------------------------------------------------------------------------+
                                                |
                                      +---------+---------+
                                      |  Kubernetes API   |
                                      |  (Deployments,    |
                                      |   Pods, Services) |
                                      +-------------------+
```

---

## 3. Topic Dependency Graph

| # | Topic                                    | Prefix   | Depends On |
|---|------------------------------------------|----------|------------|
| 1 | HTTP Server                              | HTTP     | -          |
| 2 | Health Service                           | HLT      | 1          |
| 3 | Container API Handlers                   | API      | 1, 4       |
| 4 | Kubernetes Integration & Store           | K8S, STR | -          |
| 5 | Resource Status Monitoring & Reporting   | MON      | 4          |
| 6 | DCM Registration                         | REG      | 1          |

```
Topic 1: HTTP Server              (independent)
Topic 4: K8s Integration & Store  (independent)
  |         |
  |         +---> Topic 5: Status Monitoring    (depends on 4)
  |
  +---> Topic 2: Health Service         (depends on 1)
  +---> Topic 3: Container API Handlers (depends on 1, 4)
  +---> Topic 6: DCM Registration       (depends on 1)
```

Topics 1 and 4 can be delivered in parallel. Topics 2, 3, 5, and 6 depend on
their respective prerequisites.

> **Note:** Handler tests mock the container storage interface; K8s store
> tests use `client-go/kubernetes/fake`.

---

## 4. Topic Specifications

### 4.1 HTTP Server

#### Overview

Foundation layer: chi-based HTTP server with graceful shutdown, signal handling,
configuration loading from environment variables, and route
registration for all OpenAPI-defined endpoints. Container endpoints are under
`/api/v1alpha1`, and the health endpoint is at `/health`.

Out of scope: TLS termination (handled by infrastructure/ingress),
authentication/authorization middleware, rate limiting.

#### Requirements

| ID | Requirement | Priority | Notes |
|----|-------------|----------|-------|
| REQ-HTTP-010 | The SP MUST start an HTTP server on the configured address | MUST | |
| REQ-HTTP-020 | The SP MUST register all OpenAPI-defined routes. Container endpoints under `/api/v1alpha1`, health at `/health` | MUST | DD-050 |
| REQ-HTTP-030 | The SP MUST initiate graceful shutdown on SIGTERM: stop new connections, drain in-flight requests within configured timeout, exit cleanly | MUST | |
| REQ-HTTP-040 | The SP MUST initiate graceful shutdown on SIGINT, behaving identically to REQ-HTTP-030 | MUST | |
| REQ-HTTP-050 | The SP MUST load configuration values from environment variables | MUST | |
| REQ-HTTP-060 | The SP MUST log each HTTP request at INFO level including method, path, response status code, and duration | MUST | |
| REQ-HTTP-070 | The SP MUST catch panics in HTTP handlers and return an RFC 7807 INTERNAL error response. Panics that signal intentional connection abort MUST be re-raised. If the response has already started streaming, the panic MUST be logged without writing a response body. Recovery middleware MUST be applied as the outermost middleware layer to ensure panics in any middleware are caught | MUST | |
| REQ-HTTP-080 | The SP MUST log server lifecycle events including listen address on startup | MUST | |
| REQ-HTTP-090 | The SP MUST return 400 Bad Request with RFC 7807 error body for malformed requests | MUST | |
| REQ-HTTP-091 | The API framework layer MUST return RFC 7807 error responses for request parsing and response serialization failures, not plain text | MUST | |
| REQ-HTTP-110 | The SP SHOULD enforce a configurable per-request timeout, cancelling the request context after the deadline | SHOULD | |

#### Configuration Introduced

| Config Key | Env Var | Default | Description |
|------------|---------|---------|-------------|
| server.address | SP_SERVER_ADDRESS | :8080 | Listen address (host:port) |
| server.shutdownTimeout | SP_SERVER_SHUTDOWN_TIMEOUT | 15s | Graceful shutdown drain timeout |
| server.requestTimeout | SP_SERVER_REQUEST_TIMEOUT | 30s | Per-request context timeout |

#### Acceptance Criteria

##### AC-HTTP-010: Server starts on configured address

- **Validates:** REQ-HTTP-010
- **Given** valid configuration is provided
- **When** the SP starts
- **Then** the HTTP server MUST begin listening on the configured address

##### AC-HTTP-020: Route registration

- **Validates:** REQ-HTTP-020
- **Given** the HTTP server has started
- **When** a request is made to any defined endpoint (e.g., `/health`, `/api/v1alpha1/containers`)
- **Then** the request MUST be routed to the corresponding handler

##### AC-HTTP-030: Graceful shutdown on SIGTERM

- **Validates:** REQ-HTTP-030
- **Given** the HTTP server is running
- **When** SIGTERM is received
- **Then** the server MUST stop accepting new connections
- **And** the server MUST drain in-flight requests within the configured shutdown timeout
- **And** the server MUST exit cleanly after draining or timeout

##### AC-HTTP-040: Graceful shutdown on SIGINT

- **Validates:** REQ-HTTP-040
- **Given** the HTTP server is running
- **When** SIGINT is received
- **Then** the server MUST behave identically to REQ-HTTP-030

##### AC-HTTP-050: Configuration from environment variables

- **Validates:** REQ-HTTP-050
- **Given** environment variables are set (e.g., SP_SERVER_ADDRESS=:9090)
- **When** the SP starts
- **Then** the SP MUST use the values from the environment variables

##### AC-HTTP-080: Lifecycle logging

- **Validates:** REQ-HTTP-080
- **Given** the SP starts or stops
- **When** the server begins listening or initiates shutdown
- **Then** the SP MUST log the event including the listen address on startup

##### AC-HTTP-060: Request logging

- **Validates:** REQ-HTTP-060
- **Given** any HTTP request is processed
- **When** the response is sent
- **Then** the SP MUST log at INFO level with method, path, status code, and duration

##### AC-HTTP-070: Panic recovery

- **Validates:** REQ-HTTP-070
- **Given** a handler panics during request processing
- **When** the panic is caught
- **Then** the response MUST be HTTP 500 with RFC 7807 body (type=INTERNAL)
- **And** the panic and stack trace MUST be logged at ERROR level
- **And** panics that signal intentional connection abort MUST be re-raised
- **And** if the response has already started streaming, a warning MUST be logged without writing a response body

##### AC-HTTP-090: Malformed request handling

- **Validates:** REQ-HTTP-090
- **Given** a request with invalid parameters (e.g., malformed query params)
- **When** the request reaches the router
- **Then** the SP MUST return a 400 Bad Request with an RFC 7807 error body

##### AC-HTTP-091: Framework-layer error responses

- **Validates:** REQ-HTTP-091
- **Given** the API framework layer encounters a request parsing or response serialization failure
- **When** an error response is generated
- **Then** the error response MUST be RFC 7807 with `Content-Type: application/problem+json`
- **And** INTERNAL errors MUST NOT expose implementation details

##### AC-HTTP-110: Request timeout

- **Validates:** REQ-HTTP-110
- **Given** a configurable request timeout is set (default 30s)
- **When** a request exceeds the timeout
- **Then** the request context MUST be cancelled

#### Dependencies

None - independently deliverable.

---

### 4.2 Health Service

#### Overview

Implementation of the `/health` endpoint as defined in the OpenAPI spec. This
endpoint is polled by the DCM control plane every 10 seconds to determine SP
liveness and backing provider health. The endpoint checks Kubernetes API server
reachability and reports `status: "healthy"` or `status: "unhealthy"` per the
DCM three-state health model
([enhancements#47](https://github.com/dcm-project/enhancements/pull/47)).

Out of scope: NATS connectivity checks, readiness vs liveness distinction
(future enhancement).

#### Requirements

| ID | Requirement | Priority | Notes |
|----|-------------|----------|-------|
| REQ-HLT-010 | The SP MUST expose `GET /api/v1alpha1/containers/health` and return HTTP 200 OK. The SPRM constructs health URLs as `{registered_endpoint}/health`, so the health endpoint MUST be at the resource-relative path | MUST | |
| REQ-HLT-020 | The health response MUST return a JSON body conforming to the Health schema with `status`, `type`, `path`, `version`, and `uptime` fields. The `status` field MUST be `"healthy"` when the backing K8s cluster is reachable, or `"unhealthy"` when it is not | MUST | DD-070 |
| REQ-HLT-030 | The response MUST set `Content-Type: application/json` | MUST | |
| REQ-HLT-040 | The health endpoint MUST be lightweight and return quickly, suitable for 10-second polling intervals. The only external call permitted is a Kubernetes API server version discovery request | MUST | |
| REQ-HLT-050 | The health endpoint MUST check backing K8s cluster liveness by calling the Kubernetes API server's version discovery endpoint | MUST | DD-070 |
| REQ-HLT-060 | When the K8s cluster is unreachable or the discovery call fails, the health endpoint MUST return HTTP 200 with `status: "unhealthy"`. All other response fields (`type`, `path`, `version`, `uptime`) MUST still be populated | MUST | DD-070 |
| REQ-HLT-070 | The `CheckHealth` method MUST be part of the `ContainerRepository` interface so that the store implementation is the single source of backing-infrastructure interaction | MUST | DD-040 |

#### Acceptance Criteria

##### AC-HLT-010: Health endpoint availability

- **Validates:** REQ-HLT-010
- **Given** the HTTP server is running
- **When** a GET request is made to `/api/v1alpha1/containers/health`
- **Then** the SP MUST return HTTP 200 OK

##### AC-HLT-020: Health response body — healthy

- **Validates:** REQ-HLT-020, REQ-HLT-050
- **Given** the SP is running and the backing K8s cluster is reachable
- **When** GET `/api/v1alpha1/containers/health` is called
- **Then** the response body MUST contain:
  - `status`: `"healthy"`
  - `type`: `"k8s-container-service-provider.dcm.io/health"`
  - `path`: `"health"`
  - `version`: SP build version (string)
  - `uptime`: seconds since SP started (integer)

##### AC-HLT-025: Health response body — unhealthy

- **Validates:** REQ-HLT-020, REQ-HLT-060
- **Given** the SP is running but the backing K8s cluster is unreachable
- **When** GET `/api/v1alpha1/containers/health` is called
- **Then** the response MUST be HTTP 200 OK
- **And** the response body MUST contain:
  - `status`: `"unhealthy"`
  - `type`: `"k8s-container-service-provider.dcm.io/health"`
  - `path`: `"health"`
  - `version`: SP build version (string)
  - `uptime`: seconds since SP started (integer)

##### AC-HLT-030: Health response content type

- **Validates:** REQ-HLT-030
- **Given** any call to the health endpoint
- **When** the response is returned
- **Then** the `Content-Type` header MUST be `application/json`

##### AC-HLT-040: Lightweight execution

- **Validates:** REQ-HLT-040
- **Given** the DCM control plane polls the health endpoint
- **When** the request is processed
- **Then** the handler MUST only perform a Kubernetes API server version discovery call (no resource listing, no DB queries)

##### AC-HLT-050: Reserved "health" container ID

- **Validates:** REQ-HLT-010
- **Given** the health endpoint is at `/api/v1alpha1/containers/health`
- **When** a client attempts to create a container with ID `"health"`
- **Then** the SP MUST reject the request with HTTP 400 INVALID_ARGUMENT because the ID would collide with the health endpoint path

#### Dependencies

Depends on Topic 1 (HTTP Server).

---

### 4.3 Container API Handlers

#### Overview

Implement all API operations defined in the OpenAPI specification. Wire each
endpoint to the container storage interface. Handle request validation,
response construction, and error mapping to RFC 7807 responses.

Out of scope: authentication/authorization (401/403 responses), request body
size limits.

#### Requirements

| ID | Requirement | Priority | Notes |
|----|-------------|----------|-------|
| REQ-API-010 | The SP MUST implement all API operations defined in the OpenAPI specification | MUST | |
| REQ-API-020 | POST `/api/v1alpha1/containers` MUST accept a `CreateContainerRequest` wrapper (with `spec` field) and return 201 Created with read-only fields populated as a bare Container | MUST | REQ-API-200 |
| REQ-API-030 | When no `id` query parameter is provided, the server MUST generate a UUID for the container | MUST | |
| REQ-API-040 | When an `id` query parameter is provided, the server MUST use it as the container ID | MUST | |
| REQ-API-050 | Client-specified IDs MUST be validated against AEP-122 pattern `^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$` | MUST | |
| REQ-API-060 | Newly created containers MUST have status set to PENDING | MUST | |
| REQ-API-070 | The create response MUST populate all read-only fields: `id`, `path`, `status`, `create_time`, `update_time`, `metadata.namespace` | MUST | |
| REQ-API-080 | POST MUST return 409 Conflict when a container with the same `metadata.name` already exists | MUST | SC-001 |
| REQ-API-090 | POST MUST validate required fields in the request body (e.g., `image`) | MUST | SC-002 |
| REQ-API-100 | GET `/api/v1alpha1/containers` MUST return a paginated list conforming to ContainerList schema | MUST | SC-006 |
| REQ-API-110 | GET MUST support `max_page_size` and `page_token` query parameters for pagination | MUST | |
| REQ-API-120 | GET MUST return 200 OK with an empty `containers` array when no containers exist | MUST | |
| REQ-API-130 | GET `/api/v1alpha1/containers/{containerId}` MUST return the container with 200 OK | MUST | |
| REQ-API-140 | GET MUST return 404 Not Found with RFC 7807 error body when the container does not exist | MUST | |
| REQ-API-150 | DELETE `/api/v1alpha1/containers/{containerId}` MUST return 204 No Content | MUST | |
| REQ-API-151 | A GET request for a deleted container MUST return 404 Not Found | MUST | |
| REQ-API-160 | DELETE MUST return 404 Not Found with RFC 7807 error body when the container does not exist | MUST | |
| REQ-API-170 | All error responses MUST conform to RFC 7807 with `Content-Type: application/problem+json` and at minimum `type` and `title` fields | MUST | |
| REQ-API-180 | Error types MUST map to appropriate HTTP status codes per the error mapping table | MUST | |
| REQ-API-200 | The POST `/api/v1alpha1/containers` request body MUST be a JSON object with a required `spec` property containing the Container input fields (CreateContainerRequest wrapper). The response remains a bare Container | MUST | D1 |
| REQ-API-210 | The Container schema MUST include an optional `provider_hints` field (type: object, additionalProperties: true). The SP MUST accept it on input but MUST NOT act on hint content | MUST | D3, DD-080 |

**Error type mapping (REQ-API-180):**

| Error Condition | HTTP Status | Error Type |
|-----------------|-------------|------------|
| Invalid request body | 400 | INVALID_ARGUMENT |
| Container not found | 404 | NOT_FOUND |
| Name already exists | 409 | ALREADY_EXISTS |
| Unexpected error | 500 | INTERNAL |

> **Note:** 401 and 403 responses are defined in the OpenAPI spec for forward
> compatibility but MUST NOT be returned in v1. Authentication and authorization
> are out of scope for v1.

#### Acceptance Criteria

##### AC-API-010: Create container - success

- **Validates:** REQ-API-020
- **Given** a valid Container request body
- **When** POST `/api/v1alpha1/containers` is called
- **Then** the response MUST be 201 Created
- **And** the response body MUST be the created Container with read-only fields populated

##### AC-API-020: Create container - server-generated ID

- **Validates:** REQ-API-030
- **Given** POST `/api/v1alpha1/containers` is called without `?id=`
- **When** the container is created
- **Then** the response MUST contain a server-generated UUID as the `id` field

##### AC-API-030: Create container - client-specified ID

- **Validates:** REQ-API-040
- **Given** POST `/api/v1alpha1/containers?id=my-web-app` is called
- **When** the container is created
- **Then** the response `id` field MUST be `"my-web-app"`

##### AC-API-040: Create container - client ID validation

- **Validates:** REQ-API-050
- **Given** POST `/api/v1alpha1/containers?id=INVALID_ID` is called
- **When** the ID does not match pattern `^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`
- **Then** the response MUST be 400 Bad Request with an RFC 7807 error body

##### AC-API-050: Create container - initial status

- **Validates:** REQ-API-060
- **Given** a container is created successfully
- **When** the response is returned
- **Then** the `status` field MUST be `"PENDING"`

##### AC-API-060: Create container - read-only fields

- **Validates:** REQ-API-070
- **Given** a container is created successfully
- **When** the response is returned
- **Then** the following fields MUST be populated:
  - `id`: server-generated or client-specified
  - `path`: `"containers/{containerId}"`
  - `status`: `"PENDING"`
  - `create_time`: current timestamp
  - `update_time`: current timestamp (equals `create_time` on creation; on GET, populated from the most recent condition transition time of the Pod, or Deployment status conditions if no Pod exists)
  - `metadata.namespace`: configured namespace

##### AC-API-070: Create container - conflict

- **Validates:** REQ-API-080
- **Given** a container with metadata.name "web-app" already exists
- **When** POST is called with another container with metadata.name "web-app"
- **Then** the response MUST be 409 Conflict with an RFC 7807 error body
- **And** the existing resource MUST NOT be modified

##### AC-API-080: Create container - validation

- **Validates:** REQ-API-090
- **Given** a request body missing required fields (e.g., no `image`)
- **When** POST is called
- **Then** the response MUST be 400 Bad Request with an RFC 7807 error body

##### AC-API-090: List containers - success

- **Validates:** REQ-API-100
- **Given** containers exist in the store
- **When** GET `/api/v1alpha1/containers` is called
- **Then** the response MUST be 200 OK
- **And** the body MUST conform to the ContainerList schema

##### AC-API-100: List containers - pagination

- **Validates:** REQ-API-110
- **Given** more containers exist than max_page_size
- **When** GET is called with `?max_page_size=10`
- **Then** at most 10 containers MUST be returned
- **And** `next_page_token` MUST be present if more results exist

##### AC-API-110: List containers - empty

- **Validates:** REQ-API-120
- **Given** no containers exist
- **When** GET `/api/v1alpha1/containers` is called
- **Then** the response MUST be 200 OK with an empty `containers` array

##### AC-API-120: Get container - success

- **Validates:** REQ-API-130
- **Given** a container with id "abc-123" exists
- **When** GET `/api/v1alpha1/containers/abc-123` is called
- **Then** the response MUST be 200 OK with the Container body

##### AC-API-130: Get container - not found

- **Validates:** REQ-API-140
- **Given** no container with id "xyz-999" exists
- **When** GET `/api/v1alpha1/containers/xyz-999` is called
- **Then** the response MUST be 404 Not Found with an RFC 7807 error body

##### AC-API-140: Delete container - success

- **Validates:** REQ-API-150, REQ-API-151
- **Given** a container with id "abc-123" exists
- **When** DELETE `/api/v1alpha1/containers/abc-123` is called
- **Then** the response MUST be 204 No Content with no body
- **And** subsequent GET for "abc-123" MUST return 404

##### AC-API-150: Delete container - not found

- **Validates:** REQ-API-160
- **Given** no container with id "xyz-999" exists
- **When** DELETE `/api/v1alpha1/containers/xyz-999` is called
- **Then** the response MUST be 404 Not Found with an RFC 7807 error body

##### AC-API-160: Error response format

- **Validates:** REQ-API-170
- **Given** any error condition
- **When** an error response is returned
- **Then** the response MUST have `Content-Type: application/problem+json`
- **And** the body MUST contain at minimum `type` and `title` fields

##### AC-API-200: Request body envelope accepted

- **Validates:** REQ-API-200
- **Given** POST body `{"spec": {<valid-container-fields>}}`
- **When** the request is processed
- **Then** 201 with bare Container response (no wrapper)

##### AC-API-210: Raw Container body rejected

- **Validates:** REQ-API-200
- **Given** POST body is a raw Container (no `spec` wrapper)
- **When** OpenAPI validation is applied
- **Then** 400 INVALID_ARGUMENT

##### AC-API-220: Provider hints accepted and passthrough

- **Validates:** REQ-API-210
- **Given** Create request with `"provider_hints": {"placement": "gpu-node"}`
- **When** Container is created
- **Then** 201, hints do not affect K8s resource creation

#### Dependencies

Depends on Topic 1 (HTTP Server) and Topic 4 (Kubernetes Integration & Store).

---

### 4.4 Kubernetes Integration & Store

#### Overview

Define a container storage interface that abstracts container storage
operations. Implement it backed by Kubernetes. Create and delete Deployments
(replicas=1) and optionally Services. Manage resource labeling. Support both
kubeconfig and in-cluster authentication.

Out of scope: RBAC/ServiceAccount creation (assumed pre-configured), network
policies, resource quotas/limit ranges, multi-namespace support, persistent
volume support, multi-container Pods, resource types other than Deployment,
sorting within List, watch/notification mechanism (handled by informers in
topic 5).

#### Requirements - Store Interface

| ID | Requirement | Priority | Notes |
|----|-------------|----------|-------|
| REQ-STR-010 | The SP MUST define a container storage interface with Create, Get, List, and Delete operations | MUST | DD-040 |
| REQ-STR-020 | The Create operation MUST return the created Container with all server-generated read-only fields populated | MUST | |
| REQ-STR-030 | The Create operation MUST return a conflict error if a container with the same `metadata.name` already exists | MUST | |
| REQ-STR-040 | The Get operation MUST return the matching Container for a valid containerId, or a not-found error if no match exists | MUST | |
| REQ-STR-050 | The List operation MUST accept pagination parameters (`max_page_size`, `page_token`) and return a paginated ContainerList | MUST | SC-006 |
| REQ-STR-060 | The List operation MUST default to max_page_size=50 when not specified | MUST | |
| REQ-STR-070 | The Delete operation MUST delete the container matching the containerId, or return a not-found error if no match exists | MUST | |
| REQ-STR-080 | The store MUST define typed errors for not-found and conflict conditions to enable API handlers to map them to appropriate HTTP status codes | MUST | |

#### Requirements - Kubernetes Integration

| ID | Requirement | Priority | Notes |
|----|-------------|----------|-------|
| REQ-K8S-010 | The SP MUST create a Kubernetes Deployment with replicas=1 for each container creation request | MUST | DD-010 |
| REQ-K8S-020 | All created resources (Deployment, Service) MUST carry DCM labels: `dcm.project/managed-by=dcm`, `dcm.project/dcm-instance-id=<containerId>`, `dcm.project/dcm-service-type=container` | MUST | SC-004 |
| REQ-K8S-030 | The Deployment container spec MUST use `image.reference` as the container image | MUST | |
| REQ-K8S-040 | Container CPU resources MUST be mapped to K8s resource requests and limits | MUST | DD-100 |
| REQ-K8S-050 | Container memory resources MUST be converted from schema format (MB/GB/TB) to K8s format (Mi/Gi/Ti) | MUST | DD-090 |
| REQ-K8S-060 | When `process.command` is provided, it MUST be mapped to the container spec command | MUST | |
| REQ-K8S-070 | When `process.args` is provided, it MUST be mapped to the container spec args | MUST | |
| REQ-K8S-080 | When `process.env` is provided, each entry MUST be mapped to a K8s EnvVar | MUST | |
| REQ-K8S-090 | When `network.ports` is provided, each port MUST be mapped to a container port in the Deployment | MUST | |
| REQ-K8S-100 | When any port has `visibility` != `none`, a Service MUST be created including all non-none ports | MUST | SC-005 |
| REQ-K8S-110 | All non-none ports MUST be included in a single Service resource | MUST | |
| REQ-K8S-120 | When all non-none ports have `visibility=internal`, the Service type MUST be ClusterIP | MUST | |
| REQ-K8S-125 | When any port has `visibility=external`, the Service type MUST be the configured `externalServiceType` | MUST | |
| REQ-K8S-150 | When all ports have `visibility=none` (or no ports exist), no Service MUST be created | MUST | |
| REQ-K8S-155 | When `network` is provided but `ports` is absent or null, the SP MUST treat it identically to having no ports — no Service is created and no error is returned | MUST | D2 |
| REQ-K8S-170 | The SP MUST return a conflict error if a Deployment with the same `metadata.name` already exists in the configured namespace | MUST | SC-001 |
| REQ-K8S-180 | Delete MUST remove the Deployment (cascading to Pods) and associated Service | MUST | |
| REQ-K8S-190 | Delete MUST succeed even if no Service exists for the container | MUST | |
| REQ-K8S-200 | The SP MUST support authentication via kubeconfig file | MUST | |
| REQ-K8S-210 | The SP MUST support in-cluster service account authentication | MUST | |
| REQ-K8S-220 | GET operations MUST query the cluster and populate runtime data (status, network.ip, service fields including service.name) | MUST | DD-140 |
| REQ-K8S-230 | Pod phases MUST be mapped to DCM ContainerStatus values per the status mapping table | MUST | DD-020 |
| REQ-K8S-240 | When a Deployment exists but no Pod has been created yet, the status MUST be PENDING | MUST | SC-007 |
| REQ-K8S-250 | List operations MUST support pagination over Deployment resources using K8s continue tokens | MUST | |
| REQ-K8S-260 | All resources MUST be created in the configured namespace | MUST | DD-030 |
| REQ-K8S-270 | Created Services MUST carry the same DCM labels as the Deployment | MUST | |
| REQ-K8S-280 | During a rolling update (UpdatedReplicas < Replicas), the store MUST select the Running Pod for status; if none Running, the newest Pod MUST be selected | MUST | |
| REQ-K8S-290 | If 2 Pods exist without a rolling update in progress, or 3+ Pods exist regardless, the store MUST return a ConflictError | MUST | |
| REQ-K8S-300 | Get and Delete MUST return a ConflictError when multiple Deployments match the same instance ID label | MUST | |

**Status mapping (REQ-K8S-230):**

| Pod Phase | DCM Status |
|-----------|------------|
| Pending | PENDING |
| Running | RUNNING |
| Failed | FAILED |
| Succeeded | _(ignored -- not expected for Deployments; monitoring MUST NOT publish a CloudEvent for this phase)_ |
| Unknown | UNKNOWN |

**Memory unit conversion (REQ-K8S-050):**

| Schema Format | Kubernetes Format |
|---------------|-------------------|
| MB | Mi |
| GB | Gi |
| TB | Ti |

> **Note:** Pod labels are inherited from the Deployment's pod template spec.

> **Note:** `metadata.name` is used as the `generateName` prefix for Kubernetes
> Deployments and Services (e.g., `metadata.name + "-"`). The actual resource
> name is server-assigned. `metadata.name` must comply with DNS label limits
> (63 characters maximum). The OpenAPI spec enforces `maxLength: 63` on
> `ContainerMetadata.name`. See DD-140.

#### Configuration Introduced

| Config Key | Env Var | Default | Description |
|------------|---------|---------|-------------|
| kubernetes.namespace | SP_K8S_NAMESPACE | default | Namespace for all managed resources |
| kubernetes.kubeconfig | SP_K8S_KUBECONFIG | (auto) | Path to kubeconfig (empty = in-cluster) |
| kubernetes.externalServiceType | SP_K8S_EXTERNAL_SVC_TYPE | - | Service type for `external` visibility (LoadBalancer or NodePort) |

#### Acceptance Criteria - Store Interface

##### AC-STR-010: Create operation populates read-only fields

- **Validates:** REQ-STR-020
- **Given** a valid Container is passed to Create
- **When** the operation succeeds
- **Then** the returned Container MUST have all read-only fields populated

##### AC-STR-020: Create conflict detection

- **Validates:** REQ-STR-030
- **Given** a container with metadata.name "web-app" already exists
- **When** Create is called with another container with metadata.name "web-app"
- **Then** a conflict error MUST be returned

##### AC-STR-030: Get operation - found

- **Validates:** REQ-STR-040
- **Given** a container with id "abc-123" exists
- **When** Get is called with containerId "abc-123"
- **Then** the matching Container MUST be returned

##### AC-STR-040: Get operation - not found

- **Validates:** REQ-STR-040
- **Given** no container with id "xyz-999" exists
- **When** Get is called with containerId "xyz-999"
- **Then** a not-found error MUST be returned

##### AC-STR-050: List operation - first page

- **Validates:** REQ-STR-050
- **Given** 75 containers exist and max_page_size is 50
- **When** List is called
- **Then** the first page MUST contain 50 containers
- **And** next_page_token MUST be non-empty

##### AC-STR-060: List operation - subsequent page

- **Validates:** REQ-STR-050
- **Given** a valid page_token from a previous List call
- **When** List is called with that page_token
- **Then** the next page of results MUST be returned

##### AC-STR-070: List default page size

- **Validates:** REQ-STR-060
- **Given** no max_page_size is provided
- **When** List is called
- **Then** at most 50 containers MUST be returned

##### AC-STR-080: Delete operation

- **Validates:** REQ-STR-070
- **Given** a container with id "abc-123" exists
- **When** Delete is called with containerId "abc-123"
- **Then** the container MUST be removed
- **And** subsequent Get("abc-123") MUST return not-found

##### AC-STR-090: Error type - not found

- **Validates:** REQ-STR-080
- **Given** a not-found condition occurs
- **When** the error is returned
- **Then** the error MUST be distinguishable as a not-found error

##### AC-STR-100: Error type - conflict

- **Validates:** REQ-STR-080
- **Given** a conflict condition occurs
- **When** the error is returned
- **Then** the error MUST be distinguishable as a conflict error

#### Acceptance Criteria - Kubernetes Integration

##### AC-K8S-010: Deployment creation

- **Validates:** REQ-K8S-010
- **Given** a valid container creation request
- **When** the K8s store processes the Create operation
- **Then** a Deployment MUST be created in the configured namespace with replicas=1

##### AC-K8S-020: Resource labeling

- **Validates:** REQ-K8S-020
- **Given** any resource is created
- **When** the resource is applied to the cluster
- **Then** the following labels MUST be set:
  - `dcm.project/managed-by`: `dcm`
  - `dcm.project/dcm-instance-id`: `<containerId>`
  - `dcm.project/dcm-service-type`: `container`

##### AC-K8S-030: Image mapping

- **Validates:** REQ-K8S-030
- **Given** a container with image.reference "quay.io/myapp:v1.2"
- **When** the Deployment is created
- **Then** the Pod template container image MUST be "quay.io/myapp:v1.2"

##### AC-K8S-040: CPU resource mapping

- **Validates:** REQ-K8S-040
- **Given** a container with cpu.min=1, cpu.max=2
- **When** the Deployment is created
- **Then** the container spec MUST set:
  - `resources.requests.cpu`: "1"
  - `resources.limits.cpu`: "2"

##### AC-K8S-050: Memory resource mapping

- **Validates:** REQ-K8S-050
- **Given** a container with memory.min="1GB", memory.max="2GB"
- **When** the Deployment is created
- **Then** the container spec MUST set:
  - `resources.requests.memory`: "1Gi"
  - `resources.limits.memory`: "2Gi"

##### AC-K8S-060: Process command mapping

- **Validates:** REQ-K8S-060
- **Given** a container with process.command=["/app/start"]
- **When** the Deployment is created
- **Then** the container spec `command` MUST be ["/app/start"]

##### AC-K8S-070: Process args mapping

- **Validates:** REQ-K8S-070
- **Given** a container with process.args=["--config", "/etc/config.yaml"]
- **When** the Deployment is created
- **Then** the container spec `args` MUST be ["--config", "/etc/config.yaml"]

##### AC-K8S-080: Environment variable mapping

- **Validates:** REQ-K8S-080
- **Given** a container with process.env=[{name:"ENV", value:"prod"}]
- **When** the Deployment is created
- **Then** the container spec `env` MUST contain {Name:"ENV", Value:"prod"}

##### AC-K8S-090: Port mapping

- **Validates:** REQ-K8S-090
- **Given** a container with network.ports=[{containerPort: 8080}]
- **When** the Deployment is created
- **Then** the container spec `ports` MUST contain {ContainerPort: 8080}

##### AC-K8S-100: Service creation - visibility-driven

- **Validates:** REQ-K8S-100
- **Given** a container has ports with `visibility` != `none`
- **When** the container is created
- **Then** a single Service MUST be created including all non-none ports

##### AC-K8S-110: Service creation - all non-none ports in single Service

- **Validates:** REQ-K8S-110
- **Given** a container with ports [{containerPort: 8080, visibility: internal}, {containerPort: 9090, visibility: internal}]
- **When** the Service is created
- **Then** the Service MUST have two port entries (8080 and 9090)
- **And** only one Service resource MUST be created

##### AC-K8S-120: Service type - internal-only ports

- **Validates:** REQ-K8S-120
- **Given** all non-none ports have `visibility=internal`
- **When** the Service is created
- **Then** the Service type MUST be ClusterIP

##### AC-K8S-125: Service type - external port uses externalServiceType

- **Validates:** REQ-K8S-125
- **Given** at least one port has `visibility=external`
- **And** SP configuration has externalServiceType="LoadBalancer"
- **When** the Service is created
- **Then** the Service type MUST be LoadBalancer

##### AC-K8S-150: Service creation - all ports none

- **Validates:** REQ-K8S-150
- **Given** all ports have `visibility=none` (or no ports exist)
- **When** the container is created
- **Then** no Service MUST be created

##### AC-K8S-152: Network without ports produces no Service

- **Validates:** REQ-K8S-155
- **Given** Container with `"network": {}` (ports absent)
- **When** Create is called
- **Then** Container created successfully, no Service created

##### AC-K8S-155: Visibility inference on GET - internal

- **Validates:** REQ-K8S-220
- **Given** a container has a ClusterIP Service
- **When** GET is called
- **Then** ports in the Service MUST have `visibility=internal`

##### AC-K8S-156: Visibility inference on GET - external

- **Validates:** REQ-K8S-220
- **Given** a container has a LoadBalancer Service
- **When** GET is called
- **Then** ports in the Service MUST have `visibility=external`

##### AC-K8S-157: Visibility inference on GET - none

- **Validates:** REQ-K8S-220
- **Given** a container has no Service
- **When** GET is called
- **Then** all ports MUST have `visibility=none`

##### AC-K8S-180: Deployment conflict detection

- **Validates:** REQ-K8S-170
- **Given** a Deployment named "web-app" exists in the namespace
- **When** Create is called with metadata.name "web-app"
- **Then** a conflict error MUST be returned
- **And** the existing Deployment MUST NOT be modified

##### AC-K8S-190: Cascading deletion

- **Validates:** REQ-K8S-180
- **Given** a container "abc-123" has an associated Deployment and Service
- **When** Delete is called with containerId "abc-123"
- **Then** the Deployment MUST be deleted (with cascading delete for Pods)
- **And** the associated Service MUST be deleted

##### AC-K8S-200: Delete with no Service

- **Validates:** REQ-K8S-190
- **Given** a container "abc-123" has a Deployment but no Service
- **When** Delete is called with containerId "abc-123"
- **Then** only the Deployment MUST be deleted
- **And** the operation MUST succeed

##### AC-K8S-210: Kubernetes authentication - kubeconfig

- **Validates:** REQ-K8S-200
- **Given** SP_K8S_KUBECONFIG points to a valid kubeconfig file
- **When** the SP initializes the K8s client
- **Then** the client MUST authenticate using the kubeconfig credentials

##### AC-K8S-220: Kubernetes authentication - in-cluster

- **Validates:** REQ-K8S-210
- **Given** SP_K8S_KUBECONFIG is not set
- **And** the SP is running inside a Kubernetes cluster
- **When** the SP initializes the K8s client
- **Then** the client MUST authenticate using the in-cluster service account

##### AC-K8S-230: Get operation - runtime data population

- **Validates:** REQ-K8S-220
- **Given** a container "abc-123" has a running Pod and a Service
- **When** Get is called
- **Then** the response MUST include:
  - `status`: mapped from Pod phase
  - `network.ip`: from Pod status
  - `service.name`: the Kubernetes Service resource name (server-generated via `generateName`)
  - `service.clusterIP`: from Service spec
  - `service.type`: from Service spec
  - `service.externalIP`: from LoadBalancer status (when available)
  - `service.ports`: from Service spec

##### AC-K8S-240: Status when no Pod exists

- **Validates:** REQ-K8S-240
- **Given** a Deployment exists but no Pod has been scheduled
- **When** Get is called
- **Then** the status MUST be PENDING

##### AC-K8S-250: List pagination over Deployments

- **Validates:** REQ-K8S-250
- **Given** more Deployments exist than max_page_size
- **When** List is called
- **Then** pagination MUST work correctly using K8s continue tokens mapped to page_token

##### AC-K8S-260: Namespace for all resources

- **Validates:** REQ-K8S-260
- **Given** SP configuration has namespace="production"
- **When** any resource (Deployment, Service) is created
- **Then** the resource MUST be in the "production" namespace

##### AC-K8S-270: Service labels

- **Validates:** REQ-K8S-270
- **Given** a Service is created for container "abc-123"
- **When** the Service is applied
- **Then** the Service MUST have labels:
  - `dcm.project/managed-by`: `dcm`
  - `dcm.project/dcm-instance-id`: `abc-123`
  - `dcm.project/dcm-service-type`: `container`

##### AC-K8S-280: Rolling update pod selection

- **Validates:** REQ-K8S-280
- **Given** a Deployment is mid-rollout (UpdatedReplicas < Replicas) with 2 Pods
- **When** `Get` is called
- **Then** the Running Pod MUST be selected for status; if none Running, the newest Pod MUST be selected

##### AC-K8S-290a: ConflictError for 2 pods without rollout

- **Validates:** REQ-K8S-290
- **Given** a Deployment has stable status (no rollout) but 2 Pods exist
- **When** `Get` is called
- **Then** a ConflictError MUST be returned

##### AC-K8S-290b: ConflictError for 3+ pods

- **Validates:** REQ-K8S-290
- **Given** 3 or more Pods exist for an instance, regardless of rollout state
- **When** `Get` is called
- **Then** a ConflictError MUST be returned

##### AC-K8S-300a: Get conflict for multiple Deployments

- **Validates:** REQ-K8S-300
- **Given** two Deployments share the same `dcm.project/dcm-instance-id` label
- **When** `Get` is called with that instance ID
- **Then** a ConflictError MUST be returned

##### AC-K8S-300b: Delete conflict for multiple Deployments

- **Validates:** REQ-K8S-300
- **Given** two Deployments share the same `dcm.project/dcm-instance-id` label
- **When** `Delete` is called with that instance ID
- **Then** a ConflictError MUST be returned

#### Dependencies

None - independently deliverable (Store Interface and K8s Integration are
co-delivered).

---

### 4.5 Resource Status Monitoring & Reporting

#### Overview

Watch Kubernetes Deployments and Pods via indexed resource watchers. Reconcile
status from both resource types. Publish status updates to DCM via CloudEvents
over NATS.

Out of scope: JetStream publish semantics on the SP side (stream management
is the consumer's responsibility; the SP publishes to a plain NATS subject),
historical event replay.

#### Requirements

| ID | Requirement | Priority | Notes |
|----|-------------|----------|-------|
| REQ-MON-010 | The SP MUST watch Deployment resources in the configured namespace using an indexed cache | MUST | |
| REQ-MON-020 | The SP MUST watch Pod resources in the configured namespace using an indexed cache | MUST | |
| REQ-MON-030 | Both resource watchers MUST filter resources using label selector `dcm.project/managed-by=dcm,dcm.project/dcm-service-type=container` | MUST | |
| REQ-MON-040 | Both resource watchers MUST maintain a secondary index on the `dcm.project/dcm-instance-id` label to enable fast lookups | MUST | |
| REQ-MON-050 | When a Pod exists for a container instance, the Pod status MUST take precedence over the Deployment status. When multiple Pods exist for the same instance ID (e.g., during rolling updates), the Pod with the most concerning phase (Failed > Unknown > Pending > Running > Succeeded) takes precedence. | MUST | |
| REQ-MON-060 | When a Deployment exists but no Pod exists, the Deployment status MUST be used (PENDING if Available=False; FAILED if ReplicaFailure=True or Replicas=0) | MUST | SC-007 |
| REQ-MON-070 | When neither Deployment nor Pod exists for a previously tracked instance, the status MUST be DELETED | MUST | |
| REQ-MON-080 | Status reconciliation MUST follow the status mapping table | MUST | DD-020 |
| REQ-MON-090 | Status updates MUST be published as CloudEvents v1.0 events. The required CloudEvents attributes (`id`, `source`, `type`, `subject`, `specversion`, `datacontenttype`) MUST be set. The `subject` MUST be `"dcm.container"`. The `source` MUST be `dcm/providers/{providerName}`. The `type` MUST be `dcm.status.container`. The `datacontenttype` MUST be `application/json`. The data payload MUST contain instance status | MUST | DD-060, DD-110 |
| REQ-MON-095 | The CloudEvent data payload MUST include an `id` field containing the DCM instance ID, a `status` field with the DCM status string, and a `message` field with a human-readable description | MUST | DD-110 |
| REQ-MON-100 | Status events MUST be published to NATS on the subject `dcm.container` | MUST | DD-060 |
| REQ-MON-110 | The SP MUST debounce rapid status oscillations to avoid flooding the messaging system | MUST | |
| REQ-MON-120 | The instance ID MUST be extracted from the `dcm.project/dcm-instance-id` label on the resource | MUST | |
| REQ-MON-130 | Resource watchers MUST be started as asynchronous background tasks after the HTTP server is ready | MUST | |
| REQ-MON-131 | Resource watchers MUST be stopped during graceful shutdown | MUST | |
| REQ-MON-140 | Resource watchers MUST periodically re-reconcile status for all tracked resources at the configured resync interval | MUST | |
| REQ-MON-145 | On startup, after the resource cache has completed initial synchronization, the SP MUST publish a status CloudEvent for every existing DCM-managed resource | MUST | |
| REQ-MON-150 | When status is FAILED, the message MUST include the failure reason when available (e.g., from Pod.Status.ContainerStatuses) | MUST | |
| REQ-MON-160 | Resource watchers MUST automatically reconnect after API server disconnection and resume processing events without manual intervention | MUST | |
| REQ-MON-170 | Status event publishing MUST be decoupled from the transport mechanism to allow alternative implementations | MUST | |
| REQ-MON-180 | Status event publishing MUST retry with exponential backoff on transient NATS failures, up to a configurable maximum number of attempts | MUST | |
| REQ-MON-190 | When NATS is unavailable, the SP MUST log the failure and continue operating without crashing. This applies both at startup and during runtime. The NATS connection MUST use unlimited reconnection attempts with disconnect/reconnect event logging. | MUST | DD-130 |

**Status mapping (REQ-MON-080):**

| Source | Kubernetes Condition | DCM Status |
|--------|------------------------------------------------------|------------|
| Pod | Pod.Phase = Pending | PENDING |
| Pod | Pod.Phase = Running | RUNNING |
| Pod | Pod.Phase = Failed | FAILED |
| Pod | Pod.Phase = Succeeded | _(ignored -- MUST NOT publish CloudEvent)_ |
| Pod | Pod.Phase = Unknown | UNKNOWN |
| Deployment | Deployment.Available = False AND no Pod exists | PENDING |
| Deployment | Deployment.ReplicaFailure = True OR Replicas = 0 | FAILED |
| Both | Neither Deployment nor Pod found | DELETED |

> **Note:** The SUCCEEDED status from the SP Status Reporting enhancement is
> intentionally excluded. Kubernetes Deployments are designed for long-running
> services that continuously run and restart on failure. SUCCEEDED only applies
> to resource types with a defined completion state (e.g., Jobs), which are out
> of scope for v1. This may be revisited in a later version if Jobs or similar
> resource types are supported.

> **Note:** The CloudEvent format follows the updated SP Status Reporting
> enhancement ([enhancements#37](https://github.com/dcm-project/enhancements/pull/37)).
> Instance identity is carried in the data payload's `id` field, not in the
> NATS subject or CloudEvent attributes. Known discrepancies between the
> enhancement doc and the SPRM implementation are tracked in
> `.ai/exploration/2026-03-23-14-00-status-monitoring-pr-discrepancies.md`.

#### Configuration Introduced

| Config Key | Env Var | Default | Description |
|------------|---------|---------|-------------|
| nats.url | SP_NATS_URL | (required) | NATS server URL |
| provider.name | SP_NAME | (required) | Provider name for CloudEvents |
| monitoring.debounceMs | SP_MONITOR_DEBOUNCE_MS | 500 | Debounce interval in milliseconds |
| monitoring.resyncPeriod | SP_MONITOR_RESYNC_PERIOD | 10m | Informer cache resync period |

#### Acceptance Criteria

##### AC-MON-010: Deployment resource watcher

- **Validates:** REQ-MON-010
- **Given** the SP starts with valid K8s credentials
- **When** the monitoring subsystem initializes
- **Then** an indexed resource watcher MUST be created for Deployments

##### AC-MON-020: Pod resource watcher

- **Validates:** REQ-MON-020
- **Given** the SP starts with valid K8s credentials
- **When** the monitoring subsystem initializes
- **Then** an indexed resource watcher MUST be created for Pods

##### AC-MON-030: Label selector filtering

- **Validates:** REQ-MON-030
- **Given** the resource watchers are running
- **When** resources are watched
- **Then** only resources with both labels MUST be observed

##### AC-MON-040: dcm-instance-id secondary index

- **Validates:** REQ-MON-040
- **Given** a resource watcher receives a resource event
- **When** the resource has label `dcm.project/dcm-instance-id=abc-123`
- **Then** the resource MUST be indexable by instance ID "abc-123"

##### AC-MON-050: Status reconciliation - Pod priority

- **Validates:** REQ-MON-050
- **Given** both a Deployment and Pod exist for instance "abc-123"
- **When** a status event is reconciled
- **Then** the DCM status MUST be derived from Pod.Status.Phase

##### AC-MON-051: Status reconciliation - Multi-pod worst-phase selection

- **Validates:** REQ-MON-050
- **Given** multiple Pods exist for instance "abc-123" (e.g., during a rolling update)
- **When** a status event is reconciled
- **Then** the Pod with the most concerning phase (Failed > Unknown > Pending > Running > Succeeded) MUST take precedence

##### AC-MON-060: Status reconciliation - Deployment fallback

- **Validates:** REQ-MON-060
- **Given** a Deployment exists for instance "abc-123" but no Pod exists
- **When** a status event is reconciled
- **Then** the DCM status MUST be PENDING (if Available=False)
- **Or** FAILED (if ReplicaFailure=True or Replicas=0)

##### AC-MON-070: Status reconciliation - DELETED

- **Validates:** REQ-MON-070
- **Given** a container instance "abc-123" was previously tracked
- **And** neither Deployment nor Pod exists in the cluster
- **When** a deletion event is reconciled
- **Then** the DCM status MUST be DELETED

##### AC-MON-080: CloudEvents format

- **Validates:** REQ-MON-090, REQ-MON-095
- **Given** a status change is detected for instance "abc-123" with provider name "k8s-sp"
- **When** the event is published
- **Then** the CloudEvent MUST include:
  - `specversion`: `"1.0"`
  - `id`: unique event identifier (e.g., UUID)
  - `source`: `"dcm/providers/k8s-sp"` (derived as `dcm/providers/{providerName}`)
  - `type`: `"dcm.status.container"`
  - `subject`: `"dcm.container"`
  - `datacontenttype`: `"application/json"`
  - `data`: `{"id": "abc-123", "status": "<DCM_STATUS>", "message": "<description>"}`

##### AC-MON-085: Data payload contains instance ID

- **Validates:** REQ-MON-095
- **Given** a status change is detected for instance "abc-123"
- **When** the CloudEvent data payload is constructed
- **Then** the `id` field MUST be `"abc-123"` (the DCM instance ID)
- **And** the `status` field MUST be the DCM status string
- **And** the `message` field MUST be a human-readable description

##### AC-MON-090: NATS publishing

- **Validates:** REQ-MON-100
- **Given** a status change is detected for instance "abc-123"
- **When** the event is published
- **Then** it MUST be published to NATS subject: `dcm.container`

##### AC-MON-100: Debounce logic

- **Validates:** REQ-MON-110
- **Given** multiple status changes occur within the debounce interval
- **When** events are processed
- **Then** only the last status within the debounce window MUST be published

##### AC-MON-101: Per-instance debounce isolation

- **Validates:** REQ-MON-110
- **Given** status changes occur within the debounce interval for two different instances
- **When** events are processed
- **Then** each instance's events MUST be debounced independently — rapid changes for one instance MUST NOT suppress or delay publication for another instance

##### AC-MON-110: Instance ID extraction

- **Validates:** REQ-MON-120
- **Given** a resource event is received
- **When** the handler processes it
- **Then** the `dcm-instance-id` label value MUST be used as the instance ID

##### AC-MON-120: Resource watcher lifecycle - startup

- **Validates:** REQ-MON-130
- **Given** the HTTP server has started
- **When** the monitoring subsystem starts
- **Then** resource watchers MUST run as asynchronous background tasks

##### AC-MON-130: Resource watcher lifecycle - shutdown

- **Validates:** REQ-MON-131
- **Given** the SP receives a shutdown signal
- **When** graceful shutdown begins
- **Then** resource watchers MUST be stopped

##### AC-MON-140: Cache resync

- **Validates:** REQ-MON-140
- **Given** the resource watchers are running
- **When** the resync period elapses (default: 10 minutes)
- **Then** status reconciliation MUST be re-evaluated for every resource in the local cache
- **And** status reconciliation MUST be re-evaluated for each resource

##### AC-MON-150: Initial status sync on startup

- **Validates:** REQ-MON-145
- **Given** the SP starts or restarts
- **When** the resource cache has completed initial synchronization
- **Then** a status CloudEvent MUST be published for each existing resource with labels `dcm.project/managed-by=dcm` and `dcm.project/dcm-service-type=container`
- **And** the debounce logic (REQ-MON-110) MUST apply to these initial events

##### AC-MON-160: Failure message detail

- **Validates:** REQ-MON-150
- **Given** a Pod enters Failed phase with reason "CrashLoopBackOff"
- **When** the status event is published
- **Then** the message MUST include "CrashLoopBackOff" or equivalent detail from Pod.Status.ContainerStatuses

##### AC-MON-170: Watcher resilience

- **Validates:** REQ-MON-160
- **Given** the API server connection is interrupted
- **When** the API server becomes available again
- **Then** resource watchers MUST automatically reconnect and resume event processing

##### AC-MON-180: Decoupled status publishing

- **Validates:** REQ-MON-170
- **Given** the status event publishing subsystem
- **When** status events are published
- **Then** publishing MUST be decoupled from the transport mechanism

##### AC-MON-190: Publish retry on failure

- **Validates:** REQ-MON-180
- **Given** a transient NATS failure occurs during event publishing
- **When** the publisher retries
- **Then** retries MUST use exponential backoff up to a configurable maximum

##### AC-MON-200: NATS failure handling

- **Validates:** REQ-MON-190
- **Given** NATS is unavailable
- **When** the SP attempts to publish a status event
- **Then** the failure MUST be logged at ERROR level
- **And** the SP MUST continue serving HTTP requests without crashing

#### Dependencies

Depends on Topic 4 (Kubernetes Integration & Store).

---

### 4.6 DCM Registration

#### Overview

Self-register with the DCM Service Provider Registry on startup. Registration
runs asynchronously with exponential backoff and does not block server startup.

Out of scope: de-registration on shutdown, registration status health check
integration, provider capability updates post-registration.

#### Requirements

| ID | Requirement | Priority | Notes |
|----|-------------|----------|-------|
| REQ-REG-010 | The SP MUST register with DCM on startup via `POST {dcm.registrationUrl}/providers` | MUST | |
| REQ-REG-020 | The registration payload MUST include `name`, `service_type`, `endpoint`, `operations`, `schema_version`, and optionally `display_name`, `metadata.region_code`/`metadata.zone` | MUST | |
| REQ-REG-030 | Registration MUST execute asynchronously | MUST | |
| REQ-REG-031 | Registration MUST NOT block server startup | MUST | |
| REQ-REG-040 | Registration MUST retry with exponential backoff on failure with a maximum backoff interval. Non-retryable errors (4xx client errors) MUST stop retries immediately without further attempts | MUST | |
| REQ-REG-050 | Registration failures MUST be logged | MUST | |
| REQ-REG-051 | Registration failures MUST NOT cause the SP to exit | MUST | |
| REQ-REG-060 | Registration MUST be idempotent: re-registration on restart updates the existing entry (not duplicated) | MUST | |
| REQ-REG-070 | The SP MUST use the official DCM service provider API client library for registration | MUST | |

#### Configuration Introduced

| Config Key | Env Var | Default | Description |
|------------|---------|---------|-------------|
| dcm.registrationUrl | DCM_REGISTRATION_URL | (required) | DCM SP API registration endpoint |
| provider.name | SP_NAME | (required) | Provider name |
| provider.displayName | SP_DISPLAY_NAME | (optional) | Human-readable name |
| provider.endpoint | SP_ENDPOINT | (required) | Externally reachable SP endpoint |
| provider.region | SP_REGION | (optional) | Region metadata |
| provider.zone | SP_ZONE | (optional) | Zone metadata |

#### Acceptance Criteria

##### AC-REG-010: Self-registration on startup

- **Validates:** REQ-REG-010
- **Given** the SP starts with valid DCM registration configuration
- **When** the HTTP server is ready
- **Then** a registration request MUST be sent to the DCM SP API

##### AC-REG-020: Registration payload

- **Validates:** REQ-REG-020
- **Given** a registration request is sent
- **When** the payload is constructed
- **Then** it MUST include:
  - `name`: configured provider name
  - `service_type`: `"container"`
  - `schema_version`: `"v1alpha1"`
  - `display_name`: configured display name (if set)
  - `endpoint`: `{provider.endpoint}/api/v1alpha1/containers`
  - `operations`: `["CREATE", "DELETE", "READ"]`
  - `metadata.region_code`: configured region (if set)
  - `metadata.zone`: configured zone (if set)

##### AC-REG-030: Non-blocking registration

- **Validates:** REQ-REG-030, REQ-REG-031
- **Given** the HTTP server has started
- **When** registration is initiated
- **Then** the server MUST already be accepting HTTP requests
- **And** registration MUST run concurrently

##### AC-REG-040: Exponential backoff on failure

- **Validates:** REQ-REG-040
- **Given** the DCM registration endpoint is unreachable
- **When** a registration attempt fails
- **Then** the SP MUST retry with exponential backoff
- **And** a maximum backoff interval MUST be enforced

##### AC-REG-045: Registration stops on 4xx

- **Validates:** REQ-REG-040
- **Given** the DCM registry returns a 4xx status code
- **When** a registration attempt receives this response
- **Then** the SP MUST NOT retry
- **And** MUST log the error at ERROR level
- **And** MUST continue running and serving requests

##### AC-REG-050: Registration failure logging

- **Validates:** REQ-REG-050, REQ-REG-051
- **Given** registration fails after multiple retries
- **When** the error is handled
- **Then** the error MUST be logged at an appropriate level
- **And** the SP MUST continue running and serving requests

##### AC-REG-060: Idempotent re-registration

- **Validates:** REQ-REG-060
- **Given** the SP was previously registered with DCM
- **When** the SP restarts and re-registers
- **Then** the existing registration MUST be updated (not duplicated)

##### AC-REG-070: Registration client library

- **Validates:** REQ-REG-070
- **Given** the registration subsystem is implemented
- **When** the registration request is sent
- **Then** it MUST use the official DCM service provider API client library

#### Dependencies

Depends on Topic 1 (HTTP Server).

---

## 5. Cross-Cutting Concerns

### 5.1 Resource Identity

#### Requirements

| ID | Requirement | Priority | Notes |
|----|-------------|----------|-------|
| REQ-XC-ID-010 | Two identifiers MUST be used for container resources: `id` (DCM identifier, used in URL paths and stored as `dcm.project/dcm-instance-id` label) and `metadata.name` (used as the `generateName` prefix for Kubernetes Deployments and Services; the actual K8s resource name is server-assigned) | MUST | DD-140 |
| REQ-XC-ID-020 | Conflict detection MUST be based on `metadata.name`, not `id`. Both uniqueness constraints apply independently | MUST | SC-001 |

#### Acceptance Criteria

##### AC-XC-ID-010: Dual identifier usage

- **Validates:** REQ-XC-ID-010
- **Given** a container is created with id "abc-123" and metadata.name "web-app"
- **When** the resource is stored
- **Then** `id` MUST be used in URL paths (`/containers/abc-123`) and as the `dcm.project/dcm-instance-id` label
- **And** `metadata.name` MUST be used as the `generateName` prefix for Kubernetes Deployments and Services (e.g., `"web-app-"`). The actual K8s resource name is server-assigned.

##### AC-XC-ID-020: Conflict detection based on metadata.name

- **Validates:** REQ-XC-ID-020
- **Given** a container with metadata.name "web-app" already exists
- **When** a new container with a different `id` but the same metadata.name "web-app" is created
- **Then** the request MUST be rejected with a conflict error

### 5.2 Resource Labeling

#### Requirements

| ID | Requirement | Priority | Notes |
|----|-------------|----------|-------|
| REQ-XC-LBL-010 | All Kubernetes resources managed by this SP MUST carry the DCM labels: `dcm.project/managed-by=dcm`, `dcm.project/dcm-instance-id={containerId}`, `dcm.project/dcm-service-type=container` | MUST | SC-004, DD-120 |

**Label convention:**

| Label | Value | Description |
|-------|-------|-------------|
| dcm.project/managed-by | dcm | Identifies DCM-managed resources |
| dcm.project/dcm-instance-id | {containerId} | Links resource to container ID |
| dcm.project/dcm-service-type | container | Identifies the service type |

#### Acceptance Criteria

##### AC-XC-LBL-010: DCM labels applied to all resources

- **Validates:** REQ-XC-LBL-010
- **Given** any Kubernetes resource (Deployment, Service, Pod template) is created by the SP
- **When** the resource is applied to the cluster
- **Then** it MUST carry all three DCM labels with correct values

### 5.3 Error Handling

#### Requirements

| ID | Requirement | Priority | Notes |
|----|-------------|----------|-------|
| REQ-XC-ERR-010 | All HTTP error responses MUST conform to RFC 7807 (Problem Details for HTTP APIs) using the Error schema defined in the OpenAPI spec | MUST | |
| REQ-XC-ERR-020 | Error responses MUST set `Content-Type: application/problem+json` | MUST | |
| REQ-XC-ERR-030 | Error responses SHOULD include `detail` and `instance` fields. The `instance` field SHOULD be the request URI | SHOULD | |
| REQ-XC-ERR-040 | Error responses for INTERNAL errors MUST NOT expose implementation details such as stack traces, panic messages, raw dependency error strings, file paths, or memory addresses | MUST | |

#### Acceptance Criteria

##### AC-XC-ERR-010: RFC 7807 compliance

- **Validates:** REQ-XC-ERR-010
- **Given** any error condition in the API
- **When** an error response is returned
- **Then** the body MUST conform to the RFC 7807 Error schema with at minimum `type` and `title` fields

##### AC-XC-ERR-020: Error content type

- **Validates:** REQ-XC-ERR-020
- **Given** any error response
- **When** the response is sent
- **Then** the `Content-Type` header MUST be `application/problem+json`

##### AC-XC-ERR-030: Instance field for tracing

- **Validates:** REQ-XC-ERR-030
- **Given** any error condition
- **When** the error response is returned
- **Then** the `instance` field SHOULD be set to the request URI

##### AC-XC-ERR-040: No implementation detail leakage

- **Validates:** REQ-XC-ERR-040
- **Given** an internal error occurs (unexpected store error, panic, or validation edge case)
- **When** the error response is returned
- **Then** the detail field MUST contain a generic message
- **And** the response MUST NOT contain stack traces, file paths, memory addresses, or raw internal error messages

### 5.4 Logging

#### Requirements

| ID | Requirement | Priority | Notes |
|----|-------------|----------|-------|
| REQ-XC-LOG-010 | Structured logging MUST be used throughout the application | MUST | |
| REQ-XC-LOG-020 | Log levels MUST follow the defined convention: ERROR (unrecoverable failures), WARN (recoverable issues), INFO (lifecycle events), DEBUG (detailed data) | MUST | |

**Log level convention:**

| Level | Usage |
|-------|-------|
| ERROR | Unrecoverable failures, K8s API errors |
| WARN | Recoverable issues, registration retries |
| INFO | Lifecycle events, container create/delete operations |
| DEBUG | Detailed request/response data, informer events |

#### Acceptance Criteria

##### AC-XC-LOG-010: Structured logging

- **Validates:** REQ-XC-LOG-010
- **Given** any operation occurs in the SP
- **When** the operation is logged
- **Then** the log output MUST use structured logging format

##### AC-XC-LOG-020: Log level usage

- **Validates:** REQ-XC-LOG-020
- **Given** different types of events occur
- **When** they are logged
- **Then** ERROR, WARN, INFO, and DEBUG levels MUST be used according to the defined convention

### 5.5 Configuration Management

#### Requirements

| ID | Requirement | Priority | Notes |
|----|-------------|----------|-------|
| REQ-XC-CFG-010 | All configuration MUST be loadable from environment variables | MUST | |
| REQ-XC-CFG-020 | The SP MUST fail fast on startup when required configuration values are absent or empty, returning an error before starting any subsystem | MUST | |
| REQ-XC-CFG-030 | SP_K8S_EXTERNAL_SVC_TYPE is required and MUST be one of `LoadBalancer` or `NodePort`. The SP MUST fail fast on startup if missing or invalid | MUST | |

#### Acceptance Criteria

##### AC-XC-CFG-010: Environment variable configuration

- **Validates:** REQ-XC-CFG-010
- **Given** any configuration value
- **When** the corresponding environment variable is set
- **Then** the SP MUST use the value from the environment variable

##### AC-XC-CFG-020: Fail-fast on missing required config

- **Validates:** REQ-XC-CFG-020
- **Given** a required config value (SP_NAME, SP_ENDPOINT, DCM_REGISTRATION_URL, SP_NATS_URL, or SP_K8S_EXTERNAL_SVC_TYPE) is absent or empty
- **When** the SP starts
- **Then** the SP MUST return an error identifying the missing field
- **And** MUST exit before starting the HTTP server or any subsystem

##### AC-XC-CFG-030: Fail-fast on missing or invalid ExternalServiceType

- **Validates:** REQ-XC-CFG-030
- **Given** SP_K8S_EXTERNAL_SVC_TYPE is absent, empty, or set to an invalid value (e.g., "ClusterIP", "InvalidType")
- **When** the SP starts
- **Then** the SP MUST return an error identifying the invalid configuration
- **And** MUST exit before starting the HTTP server or any subsystem

---

## 6. Consolidated Configuration Reference

All configuration is loaded from environment variables.

| Config Key | Env Var | Default | Required | Topic |
|------------|---------|---------|----------|-------|
| server.address | SP_SERVER_ADDRESS | :8080 | No | 1 |
| server.shutdownTimeout | SP_SERVER_SHUTDOWN_TIMEOUT | 15s | No | 1 |
| server.requestTimeout | SP_SERVER_REQUEST_TIMEOUT | 30s | No | 1 |
| kubernetes.namespace | SP_K8S_NAMESPACE | default | No | 4 |
| kubernetes.kubeconfig | SP_K8S_KUBECONFIG | (auto) | No | 4 |
| kubernetes.externalServiceType | SP_K8S_EXTERNAL_SVC_TYPE | - | Yes | 4 |
| nats.url | SP_NATS_URL | - | Yes | 5 |
| provider.name | SP_NAME | - | Yes | 5, 6 |
| monitoring.debounceMs | SP_MONITOR_DEBOUNCE_MS | 500 | No | 5 |
| monitoring.resyncPeriod | SP_MONITOR_RESYNC_PERIOD | 10m | No | 5 |
| dcm.registrationUrl | DCM_REGISTRATION_URL | - | Yes | 6 |
| provider.displayName | SP_DISPLAY_NAME | (optional) | No | 6 |
| provider.endpoint | SP_ENDPOINT | - | Yes | 6 |
| provider.region | SP_REGION | (optional) | No | 6 |
| provider.zone | SP_ZONE | (optional) | No | 6 |

---

## 7. Design Decisions

### DD-010: Deployments over bare Pods

**Decision:** Use Kubernetes Deployments with replicas=1 instead of bare Pods.

**Rationale:** Deployments provide automatic restart on container failure and
recreation on node failure. DCM container instances should behave like resilient
cloud services. The added monitoring complexity (layered informers) is justified
by operational resilience.

**Related requirements:** REQ-K8S-010

### DD-020: SUCCEEDED status excluded

**Decision:** The SUCCEEDED status from the SP Status Reporting enhancement is
intentionally excluded from v1.

**Rationale:** Kubernetes Deployments are designed for long-running services.
SUCCEEDED only applies to resource types with a defined completion state (e.g.,
Jobs), which are out of scope. May be added in a later version if Jobs or
similar resource types are supported.

**Related requirements:** REQ-K8S-230, REQ-MON-080

### DD-030: Single namespace

**Decision:** All resources are created in a single configured namespace.

**Rationale:** Simplifies RBAC, resource discovery, and informer setup. May
evolve to per-container or multi-namespace in future versions.

**Related requirements:** REQ-K8S-260

### DD-040: Repository pattern for testability

**Decision:** Define a container storage interface with in-memory
implementation for testing.

**Rationale:** Enables testing API handlers without a real Kubernetes cluster.
Keeps a clean separation between HTTP layer and storage/K8s layer.

**Related requirements:** REQ-STR-010

### DD-050: Health endpoint path

**Decision:** Health is served at both `/health` (root, for direct access and
readiness probing) and `/api/v1alpha1/containers/health` (resource-relative, for
DCM health check integration which derives the health URL as
`registered_endpoint + '/health'`).

**Rationale:** The health endpoint is an infrastructure concern polled by the DCM
control plane and is not part of the versioned resource API. The OpenAPI spec
uses explicit full paths (e.g., `/api/v1alpha1/containers`) instead of a shared
server prefix, allowing `/health` to live at the root alongside versioned
resource endpoints. The resource-relative path is needed because DCM's
`healthcheck/monitor.go` constructs health URLs as `endpoint + "/health"`, and
the registered endpoint is `{base}/api/v1alpha1/containers`.

**Related requirements:** REQ-HTTP-020, REQ-HLT-010

### DD-060: NATS for messaging

**Decision:** Use NATS for CloudEvents status reporting. The SP publishes to a
plain NATS subject (`dcm.container`). The consumer (SPRM) configures a JetStream
stream that captures messages on `dcm.*` subjects, providing at-least-once
delivery on the consumer side. From the SP's perspective, publishing is
at-most-once (fire-and-forget to the NATS subject).

**Rationale:** The SP does not manage JetStream streams or consumers — that is the
SPRM's responsibility. This keeps the SP simple and decoupled from JetStream
configuration. If NATS is available, messages are delivered; if not, the SP's
retry mechanism (REQ-MON-180) handles transient failures.

**Related requirements:** REQ-MON-100

### DD-110: Instance ID in CloudEvent data payload

**Decision:** The DCM instance ID is carried in the CloudEvent data payload's
`id` field, not in the NATS subject or CloudEvent envelope attributes.

**Rationale:** The NATS subject uses a simple service-type-based hierarchy
(`dcm.container`) without per-instance subjects. This allows a single wildcard
subscription (`dcm.*`) on the consumer side. The instance ID is extracted from
the data payload by the consumer. This aligns with the SP Status Reporting
enhancement ([enhancements#37](https://github.com/dcm-project/enhancements/pull/37))
and the SPRM implementation
([service-provider-manager#33](https://github.com/dcm-project/service-provider-manager/pull/33)).

**Related requirements:** REQ-MON-095, REQ-MON-100

### DD-120: Namespaced label keys

**Decision:** All DCM label keys use the `dcm.project/` namespace prefix
(e.g., `dcm.project/managed-by`, `dcm.project/dcm-instance-id`,
`dcm.project/dcm-service-type`).

**Rationale:** Aligns with the kubevirt SP label convention. Namespaced labels
follow Kubernetes best practices by avoiding collisions with user-defined labels
and clearly identifying the owner of each label. The `dcm.project` DNS subdomain
is a valid Kubernetes label prefix.

**Related requirements:** REQ-K8S-020, REQ-XC-LBL-010

### DD-130: NATS startup resilience

**Decision:** The NATS connection uses `RetryOnFailedConnect(true)` and
`MaxReconnects(-1)` so the SP can start even when NATS is unreachable.
Disconnect and reconnect events are logged via `slog`.

**Rationale:** REQ-MON-190 requires the SP to continue operating without
crashing when NATS is unavailable. This extends to startup: the SP should serve
HTTP requests while NATS connects in the background. The `RetryOnFailedConnect`
option makes `nats.Connect` return a valid connection object immediately,
allowing the SP to proceed with HTTP server startup. Subsequent publish failures
are handled by the existing retry mechanism (REQ-MON-180).

**Related requirements:** REQ-MON-190

### DD-140: generateName for Kubernetes resources

**Decision:** Kubernetes Deployments and Services use `generateName`
(`metadata.name + "-"`) instead of a fixed `Name` field. The actual resource
name is server-assigned by the Kubernetes API.

**Rationale:** Using `generateName` avoids name collisions when multiple
containers share the same `metadata.name` with different DCM instance IDs. It
also decouples the user-facing `metadata.name` from the internal Kubernetes
resource name. Because the caller cannot predict the generated Service name,
the `ServiceInfo` schema includes a `name` field populated on GET so clients
can discover it.

**Related requirements:** REQ-K8S-010, REQ-K8S-100, REQ-K8S-220, REQ-XC-ID-010

### DD-070: Health response schema and three-state model

**Decision:** Health response uses `status: "healthy"` or `status: "unhealthy"`
with AEP fields (`type`, `path`) plus operational fields (`version`, `uptime`).
The SP checks backing K8s cluster liveness via the API server's version
discovery endpoint (`client.Discovery().ServerVersion()`).

**Rationale:** Aligns with the DCM three-state health model
([enhancements#47](https://github.com/dcm-project/enhancements/pull/47)):
`healthy` → DCM marks provider Ready; `unhealthy` → DCM marks provider
Unhealthy; non-200/timeout → DCM marks provider Unavailable after threshold.
Server version discovery is the lightest possible K8s API call.

**Related requirements:** REQ-HLT-020, REQ-HLT-050, REQ-HLT-060

### DD-080: Service creation driven by port visibility

**Decision:** Service creation is derived from per-port `visibility` enum
(`none`, `internal`, `external`) defined upstream in the catalog-manager
`ContainerPort` schema. The `createService` config and `providerHints` mechanism
are removed.

**Rationale:** Upstream alignment. The catalog-manager defines what DCM sends;
this provider must accept exactly that. Per-port visibility is more expressive
than a boolean toggle and removes the need for provider-specific hints.

**Related requirements:** REQ-K8S-100, REQ-K8S-120, REQ-K8S-125, REQ-K8S-150

### DD-090: Memory unit conversion simplification

**Decision:** MB/GB/TB mapped directly to Mi/Gi/Ti (mebibytes/gibibytes/tebibytes).

**Rationale:** Deliberate simplification; the ~7% difference between decimal
(MB) and binary (Mi) units is accepted. Not byte-exact. Keeps the API simple
for DCM consumers who use decimal units.

**Related requirements:** REQ-K8S-050

### DD-100: Integer-only CPU values

**Decision:** CPU uses integer values (minimum 1). Fractional CPU is not
supported in v1.

**Rationale:** Aligns with Service Type Definitions which define CPU as integer
cores. Fractional CPU (e.g., 500m) may be added in a later version.

**Related requirements:** REQ-K8S-040

---

## 8. Spec Clarifications

The following clarifications resolve ambiguities discovered during test-plan
gap analysis. Each is rooted in an existing requirement or the OpenAPI contract.

### SC-001: `id` uniqueness

**Related requirements:** REQ-API-080, REQ-K8S-170, REQ-XC-ID-020

A Create request MUST be rejected with 409 Conflict when the supplied
`id` (via `?id=`) matches the `dcm.project/dcm-instance-id` of an existing container, in
addition to the existing `metadata.name` conflict check. Both uniqueness
constraints apply independently.

### SC-002: `min > max` for CPU/memory

**Related requirements:** REQ-API-090

A Create request MUST be rejected with 400 Bad Request when
`resources.cpu.min > resources.cpu.max` or
`resources.memory.min > resources.memory.max`. The API treats this as an
invalid argument.

### SC-003: Service creation atomicity

**Related requirements:** REQ-K8S-100

If the Service fails to create after the Deployment was already applied, the
Deployment MUST be rolled back (deleted).

### SC-004: `metadata.labels` on Kubernetes resources

**Related requirements:** REQ-K8S-020, REQ-XC-LBL-010

User-specified `metadata.labels` from the Container request body MUST be
applied to the created Kubernetes resources (Deployment, Service, Pod template).
However, if any user-specified label key collides with a DCM-reserved label
(`dcm.project/managed-by`, `dcm.project/dcm-instance-id`, `dcm.project/dcm-service-type`), the Create request MUST
be rejected with 400 Bad Request.

### SC-005: Empty or absent `network.ports`

**Related requirements:** REQ-K8S-100, REQ-K8S-150, REQ-K8S-155

An explicit empty array `network.ports: []` MUST be treated identically to the
field being absent. A `network: {}` object with no `ports` key MUST also be
treated identically — no Service is created and no error is returned. No Service
is created when there are no ports.

### SC-006: Invalid `page_token`

**Related requirements:** REQ-STR-050, REQ-API-100

When a `page_token` that cannot be decoded or is otherwise invalid is provided
to a List operation, the SP MUST return 400 Bad Request with an RFC 7807 error
body.

### SC-007: Deployment `Available=True` with no Pod

**Related requirements:** REQ-MON-060, REQ-K8S-240

In the unlikely edge case where a Deployment reports `Available=True` but no
Pod exists, the status MUST remain PENDING. Pod existence takes priority; when
no Pod is found, the status is always PENDING regardless of Deployment
conditions (unless ReplicaFailure=True or Replicas=0, which map to FAILED).

---

## 9. Requirement ID Index

| Prefix | Topic | Count |
|--------|-------|-------|
| REQ-HTTP-NNN | 4.1: HTTP Server | 11 |
| REQ-HLT-NNN | 4.2: Health Service | 7 |
| REQ-API-NNN | 4.3: Container API Handlers | 21 |
| REQ-STR-NNN | 4.4: Store Interface | 8 |
| REQ-K8S-NNN | 4.4: Kubernetes Integration | 29 |
| REQ-MON-NNN | 4.5: Status Monitoring | 22 |
| REQ-REG-NNN | 4.6: DCM Registration | 9 |
| REQ-XC-ID-NNN | 5.1: Resource Identity | 2 |
| REQ-XC-LBL-NNN | 5.2: Resource Labeling | 1 |
| REQ-XC-ERR-NNN | 5.3: Error Handling | 4 |
| REQ-XC-LOG-NNN | 5.4: Logging | 2 |
| REQ-XC-CFG-NNN | 5.5: Configuration Management | 2 |
| **Total** | | **117** |
