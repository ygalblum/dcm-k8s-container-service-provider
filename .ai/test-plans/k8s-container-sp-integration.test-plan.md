# Test Plan: K8s Container SP — Integration Tests

## Overview

- **Related Spec:** .ai/specs/k8s-container-sp.spec.md
- **Related Requirements:** REQ-HTTP-010–040, REQ-HTTP-060–070, REQ-HTTP-080–090, REQ-HTTP-110, REQ-API-070, REQ-API-180, REQ-STR-020–070, REQ-STR-080, REQ-K8S-010–270, REQ-MON-010–030, REQ-MON-040–080, REQ-MON-095, REQ-MON-100, REQ-MON-110, REQ-MON-130–150, REQ-MON-160, REQ-MON-180–190, REQ-REG-010–070, REQ-XC-ID-010–020, REQ-XC-LBL-010, REQ-XC-ERR-010–020, REQ-XC-LOG-010–020
- **Framework:** Ginkgo v2 + Gomega
- **Created:** 2026-02-17
- **Last Updated:** 2026-04-29 (updated TC-I009/I030/I033/I093/I094 for generateName and service.name)

Integration tests verify components working together with realistic (but
controlled) dependencies. The K8s store tests use `client-go/kubernetes/fake`.
Monitoring tests use the fake K8s client with real informers. Registration tests
use a local `httptest.Server` as a mock DCM registry. NATS tests use an embedded
or mock NATS server. E2E placeholders are marked and require a real cluster.

### Utility Transitive Coverage

Several integration tests transitively exercise utility functions whose TC-IDs
are defined in the unit test plan. Where applicable, each test case lists a
**"Transitively covers"** field referencing the utility TC-IDs it exercises.
See the unit test plan's [Utility Test Case Index](k8s-container-sp-unit.test-plan.md#utility-test-case-index)
for full descriptions.

---

## 1 · HTTP Server Lifecycle

> **Suggested Ginkgo structure:** `Describe("HTTP Server")`

### TC-I001: Server starts and listens on configured address

- **Requirement:** REQ-HTTP-010
- **Priority:** High
- **Type:** Integration
- **Given:** Valid configuration with `server.address=":0"` (OS-assigned port)
- **When:** The server starts
- **Then:** HTTP connections are accepted on the assigned port AND a health check returns 200

### TC-I002: All OpenAPI-defined routes are registered

- **Requirement:** REQ-HTTP-020
- **Priority:** High
- **Type:** Integration
- **Given:** The server is running
- **When:** Requests are made to each defined endpoint
- **Then:**
  - `GET /api/v1alpha1/containers/health` does not return 404 or 405
  - `GET /api/v1alpha1/containers` does not return 404 or 405
  - `POST /api/v1alpha1/containers` does not return 404 or 405
  - `GET /api/v1alpha1/containers/test-id` does not return 404 or 405
  - `DELETE /api/v1alpha1/containers/test-id` does not return 404 or 405

### TC-I003: Undefined routes return appropriate error

- **Requirement:** REQ-HTTP-020
- **Priority:** Medium
- **Type:** Integration
- **Given:** The server is running
- **When:** `GET /undefined-path` is called
- **Then:** Response status is 404 or 405

### TC-I004: Server shuts down gracefully on SIGTERM

- **Requirement:** REQ-HTTP-030
- **Priority:** High
- **Type:** Integration
- **Given:** The server is running and a long-running request is in flight
- **When:** SIGTERM is sent to the process
- **Then:** The in-flight request completes successfully AND new connections are refused after signal AND the server process exits cleanly

### TC-I005: Server shuts down gracefully on SIGINT

- **Requirement:** REQ-HTTP-040
- **Priority:** High
- **Type:** Integration
- **Given:** The server is running
- **When:** SIGINT is sent to the process
- **Then:** Shutdown behavior is identical to SIGTERM (TC-I004)

### TC-I006: Server logs startup with listen address

- **Requirement:** REQ-HTTP-080
- **Priority:** Medium
- **Type:** Integration
- **Given:** The server starts on a configured address
- **When:** Startup completes
- **Then:** A structured log entry contains the listen address

### TC-I007: Server logs shutdown event

- **Requirement:** REQ-HTTP-080
- **Priority:** Medium
- **Type:** Integration
- **Given:** The server is running
- **When:** Shutdown is initiated
- **Then:** A structured log entry indicates shutdown has begun

### TC-I008: Malformed requests return 400 with RFC 7807 body

- **Requirement:** REQ-HTTP-090
- **Priority:** High
- **Type:** Integration (table-driven)
- **Transitively covers:** TC-U057 (max_page_size boundary and containerId enforcement), TC-U058 (invalid containerId path parameter)
- **Given:** The server is running
- **When:** Requests with malformed or out-of-range parameters are sent:
  - `max_page_size=not-a-number` (non-numeric)
  - `max_page_size=0` (below minimum) (TC-U057)
  - `max_page_size=-1` (negative) (TC-U057)
  - `max_page_size=1001` (above maximum) (TC-U057)
  - `GET /api/v1alpha1/containers/` with empty containerId (TC-U058)
- **Then:** Each response is `400` AND body is RFC 7807 error with `Content-Type: application/problem+json`

### TC-I082: Startup-notification callback failure does not crash the server

- **Requirement:** REQ-HTTP-010
- **Priority:** High
- **Type:** Integration
- **Given:** A server configured with a startup-notification callback that triggers a fatal error
- **When:** The server starts and the startup-notification callback is invoked
- **Then:** The server recovers from the callback failure AND continues accepting HTTP requests AND the failure is logged at ERROR level

### TC-I079: Shutdown timeout force-terminates hung requests

- **Requirement:** REQ-HTTP-030
- **Priority:** Medium
- **Type:** Integration
- **Given:** The server is running with a short shutdown timeout (e.g., 1 second) AND a request handler is artificially blocked (will not complete within the timeout)
- **When:** SIGTERM is sent to the process
- **Then:** The server waits for the shutdown timeout AND then force-terminates AND exits (does not hang indefinitely)

### TC-I085: onReady callback invoked only after server is serving

- **Requirement:** REQ-HTTP-010
- **Priority:** High
- **Type:** Integration
- **Given:** A server configured with an `onReady` callback
- **When:** The server starts
- **Then:** The `onReady` callback MUST NOT be invoked until the server is confirmed to be accepting HTTP connections (readiness probe succeeds)

### TC-I096: Request logging — successful request

- **Requirement:** REQ-HTTP-060
- **Priority:** High
- **Type:** Integration
- **Given:** A running server with request logging middleware
- **When:** `GET /api/v1alpha1/containers/health` is called
- **Then:** The server log MUST contain an entry with method=GET, path=/api/v1alpha1/containers/health, status=200, and duration > 0

### TC-I097: Request logging — error request

- **Requirement:** REQ-HTTP-060
- **Priority:** High
- **Type:** Integration
- **Given:** A running server with request logging middleware
- **When:** `GET /api/v1alpha1/containers/nonexistent-id` is called
- **Then:** The server log MUST contain an entry with status=404

### TC-I098: Request timeout cancels long-running requests

- **Requirement:** REQ-HTTP-110
- **Priority:** Medium
- **Type:** Integration
- **Given:** A server configured with a 200ms request timeout and a handler that blocks indefinitely
- **When:** A request is made to the blocking handler
- **Then:** The request context MUST be cancelled after approximately 200ms

### TC-I102: Recovery middleware is outermost (catches middleware panics)

- **Requirement:** REQ-HTTP-070
- **Priority:** High
- **Type:** Integration
- **Given:** A server is running with recovery middleware applied as the outermost layer
- **When:** The middleware ordering is inspected (recovery → logging → timeout → validation)
- **Then:** Recovery middleware MUST be outermost to catch panics from any inner middleware
- **Note:** This is an architectural guarantee verified by the existing panic recovery tests (TC-I080). The ordering is enforced by code inspection and the fact that handler panics are correctly caught.

### TC-I103: Health endpoint at resource-relative path

- **Requirement:** REQ-HLT-010
- **AC:** AC-HLT-010
- **Priority:** High
- **Type:** Integration
- **Given:** The server is running
- **When:** `GET /api/v1alpha1/containers/health` is called
- **Then:** The response MUST be HTTP 200 with a valid Health JSON body containing `status`, `type`, `path`, `version`, and `uptime` fields

### TC-I118: Health endpoint returns "unhealthy" when K8s is unreachable

- **Requirement:** REQ-HLT-060
- **AC:** AC-HLT-025
- **Priority:** High
- **Type:** Integration
- **Given:** The server is running with a `ContainerRepository` whose `CheckHealth` returns an error (simulating K8s unavailability)
- **When:** `GET /api/v1alpha1/containers/health` is called
- **Then:** The response MUST be HTTP 200 AND `status` is `"unhealthy"` AND all other fields (`type`, `path`, `version`, `uptime`) are still present

### TC-I104: Panic recovery returns RFC 7807 JSON

- **Requirement:** REQ-HTTP-070
- **Priority:** High
- **Type:** Integration
- **Given:** A handler that panics with a string value during request processing
- **When:** The request is processed
- **Then:** The response MUST be HTTP 500 with `Content-Type: application/problem+json` AND body contains `type=INTERNAL` AND no panic details leak to the client

### TC-I105: http.ErrAbortHandler is re-panicked

- **Requirement:** REQ-HTTP-070
- **Priority:** High
- **Type:** Integration
- **Given:** A handler that panics with `http.ErrAbortHandler`
- **When:** The request is processed
- **Then:** The connection MUST be aborted (transport-level error, not a 500 response) AND "panic recovered" MUST NOT appear in the log

### TC-I106: Headers-already-sent panic logs without writing response

- **Requirement:** REQ-HTTP-070
- **Priority:** High
- **Type:** Integration
- **Given:** A handler that writes a status header (418) and then panics
- **When:** The request is processed
- **Then:** The client sees the original 418 status AND Content-Type is NOT `application/problem+json` AND the log contains "panic recovered" and "headers already sent"

---

## 2 · K8s Store — Create Operations

> **Suggested Ginkgo structure:** `Describe("K8s Store")` → `Describe("Create")`
> All tests use `client-go/kubernetes/fake`.

### TC-I009: Create produces a Deployment with replicas=1

- **Requirement:** REQ-K8S-010, REQ-STR-020
- **Priority:** High
- **Type:** Integration
- **Transitively covers:** TC-U024 (ContainerRepository interface satisfied — `var _ ContainerRepository = (*K8sStore)(nil)` in test file)
- **Given:** A valid container request with `metadata.name="my-app"`, `image.reference="nginx:latest"`
- **When:** `Create` is called on the K8s store
- **Then:** A Deployment with `generateName` prefix `"my-app-"` exists in the configured namespace with `replicas=1` AND the returned container has all read-only fields populated (`id`, `path`, `status=PENDING`, `create_time`, `update_time`, `metadata.namespace`)

### TC-I010: Created Deployment and Pod template carry DCM labels

- **Requirement:** REQ-K8S-020
- **Priority:** High
- **Type:** Integration
- **Given:** A container with id `"abc-123"` and `metadata.name="my-app"`
- **When:** `Create` is called
- **Then:** The Deployment has labels `dcm.project/managed-by=dcm`, `dcm.project/dcm-instance-id=abc-123`, `dcm.project/dcm-service-type=container` AND the Pod template spec has the same three labels

### TC-I011: Deployment uses the specified container image

- **Requirement:** REQ-K8S-030
- **Priority:** High
- **Type:** Integration
- **Given:** A container with `image.reference="quay.io/myapp:v1.2"`
- **When:** `Create` is called
- **Then:** The Pod template container image is `"quay.io/myapp:v1.2"`

### TC-I012: CPU resources map to Kubernetes requests and limits

- **Requirement:** REQ-K8S-040
- **Priority:** High
- **Type:** Integration
- **Transitively covers:** TC-U027 (CPU values map to Kubernetes resource quantities)
- **Given:** A container with `cpu.min=1`, `cpu.max=2`
- **When:** `Create` is called
- **Then:** Container spec `resources.requests.cpu` is `"1"` AND `resources.limits.cpu` is `"2"`

### TC-I013: Memory resources convert and map correctly

- **Requirement:** REQ-K8S-050
- **Priority:** High
- **Type:** Integration
- **Transitively covers:** TC-U028 (Memory units convert from schema format to Kubernetes format)
- **Given:** A container with `memory.min="1GB"`, `memory.max="2GB"`
- **When:** `Create` is called
- **Then:** Container spec `resources.requests.memory` is `"1Gi"` AND `resources.limits.memory` is `"2Gi"`

### TC-I014: Process command maps to container spec command

- **Requirement:** REQ-K8S-060
- **Priority:** High
- **Type:** Integration
- **Given:** A container with `process.command=["/app/start"]`
- **When:** `Create` is called
- **Then:** Container spec `command` is `["/app/start"]`

### TC-I015: Process args map to container spec args

- **Requirement:** REQ-K8S-070
- **Priority:** High
- **Type:** Integration
- **Given:** A container with `process.args=["--config", "/etc/config.yaml"]`
- **When:** `Create` is called
- **Then:** Container spec `args` is `["--config", "/etc/config.yaml"]`

### TC-I016: Environment variables map to container spec env

- **Requirement:** REQ-K8S-080
- **Priority:** High
- **Type:** Integration
- **Given:** A container with `process.env=[{name:"ENV", value:"prod"}, {name:"LOG_LEVEL", value:"debug"}]`
- **When:** `Create` is called
- **Then:** Container spec `env` contains `EnvVar{Name:"ENV", Value:"prod"}` AND `EnvVar{Name:"LOG_LEVEL", Value:"debug"}`

### TC-I017: Network ports map to container spec ports

- **Requirement:** REQ-K8S-090
- **Priority:** High
- **Type:** Integration
- **Given:** A container with `network.ports=[{containerPort: 8080}, {containerPort: 9090}]`
- **When:** `Create` is called
- **Then:** Container spec `ports` contains `ContainerPort{ContainerPort: 8080}` AND `ContainerPort{ContainerPort: 9090}`

### TC-I018: Optional fields omitted when not provided

- **Requirement:** REQ-K8S-060, REQ-K8S-070, REQ-K8S-080, REQ-K8S-090
- **Priority:** Medium
- **Type:** Integration
- **Given:** A container with only required fields (no `process`, no `network`)
- **When:** `Create` is called
- **Then:** Container spec has no `command`, no `args`, no `env`, no `ports`

### TC-I069: Create rejects duplicate dcm.project/dcm-instance-id

- **Requirement:** REQ-K8S-170 (extended to `id` uniqueness — see SC-001)
- **Priority:** High
- **Type:** Integration
- **Given:** A container with `dcm.project/dcm-instance-id="existing-id"` already exists in the namespace
- **When:** `Create` is called with a different `metadata.name` but the same `id="existing-id"`
- **Then:** A conflict error is returned AND no new Deployment is created

### TC-I070: Create applies user-specified metadata.labels

- **Requirement:** REQ-K8S-020 (extended — see SC-004)
- **Priority:** High
- **Type:** Integration
- **Given:** A container with `metadata.labels: {"env": "staging", "team": "platform"}`
- **When:** `Create` is called
- **Then:** The Deployment has labels `env=staging`, `team=platform` in addition to the three DCM labels AND the Pod template spec has the same combined labels

### TC-I071: Create rejects label collision with DCM labels

- **Requirement:** REQ-K8S-020 (extended — see SC-004)
- **Priority:** High
- **Type:** Integration
- **Transitively covers:** TC-U049 (CreateContainer rejects metadata.labels colliding with DCM labels)
- **Given:** A container with `metadata.labels: {"dcm.project/managed-by": "custom-value"}`
- **When:** `Create` is called
- **Then:** An error is returned indicating the label key collides with a DCM-reserved label AND no Deployment is created

---

## 3 · K8s Store — Service Creation

> **Suggested Ginkgo structure:** `Describe("K8s Store")` → `Describe("Service Creation")`

### TC-I019: Service created for port with internal visibility

- **Requirement:** REQ-K8S-100
- **Priority:** High
- **Type:** Integration
- **Given:** Container has port `{containerPort: 8080, visibility: internal}`
- **When:** `Create` is called
- **Then:** A ClusterIP Service is created exposing port 8080

### TC-I022: Multiple internal ports in single Service

- **Requirement:** REQ-K8S-110
- **Priority:** High
- **Type:** Integration
- **Given:** Container has ports `[{containerPort: 8080, visibility: internal}, {containerPort: 9090, visibility: internal}]`
- **When:** `Create` is called
- **Then:** Exactly one Service is created AND it has two port entries (8080 and 9090)

### TC-I090: Multi-port Service has named ports for K8s compliance

- **Requirement:** REQ-K8S-110
- **Priority:** High
- **Type:** Integration
- **Given:** Container has ports `[{containerPort: 8080, visibility: internal}, {containerPort: 9090, visibility: internal}, {containerPort: 3000, visibility: internal}]`
- **When:** `Create` is called
- **Then:** Each ServicePort has a unique name (port-8080, port-9090, port-3000)

### TC-I023: Internal-only ports produce ClusterIP Service

- **Requirement:** REQ-K8S-120
- **Priority:** High
- **Type:** Integration
- **Given:** All non-none ports have `visibility=internal`
- **When:** `Create` is called
- **Then:** Service type is `ClusterIP`

### TC-I024: External port uses ExternalServiceType=LoadBalancer

- **Requirement:** REQ-K8S-125
- **Priority:** High
- **Type:** Integration
- **Given:** Container has port `{containerPort: 8080, visibility: external}` AND config has `externalServiceType="LoadBalancer"`
- **When:** `Create` is called
- **Then:** Service type is `LoadBalancer`

### TC-I025: All ports visibility=none produces no Service

- **Requirement:** REQ-K8S-150
- **Priority:** High
- **Type:** Integration
- **Given:** Container has ports with `visibility=none`
- **When:** `Create` is called
- **Then:** No Service is created

### TC-I026: No ports produces no Service

- **Requirement:** REQ-K8S-150
- **Priority:** High
- **Type:** Integration
- **Given:** Container has no `network.ports`
- **When:** `Create` is called
- **Then:** No Service is created AND Deployment is created successfully

### TC-I111: Network without ports creates no Service

- **Requirement:** REQ-K8S-155
- **AC:** AC-K8S-152
- **Priority:** High
- **Type:** Integration
- **Transitively covers:** TC-U059 (network object without ports field accepted)
- **Given:** Container with `"network": {}` (ports absent)
- **When:** `Create` is called
- **Then:** Deployment is created successfully AND no Service is created

### TC-I112: Provider hints do not affect K8s resources

- **Requirement:** REQ-API-210
- **AC:** AC-API-220
- **Priority:** High
- **Type:** Integration
- **Given:** Container with `"provider_hints": {"placement": "gpu-node"}` and a port with `visibility=internal`
- **When:** `Create` is called
- **Then:** Deployment is created normally AND Service is created as expected AND hints do not alter any K8s resource

### TC-I027: Service carries DCM labels with internal visibility

- **Requirement:** REQ-K8S-270
- **Priority:** High
- **Type:** Integration
- **Given:** Container with id `"abc-123"` AND port with `visibility=internal`
- **When:** `Create` is called
- **Then:** Service has labels `dcm.project/managed-by=dcm`, `dcm.project/dcm-instance-id=abc-123`, `dcm.project/dcm-service-type=container`

### TC-I074: External port uses ExternalServiceType=NodePort

- **Requirement:** REQ-K8S-125
- **Priority:** Medium
- **Type:** Integration
- **Given:** Container has port `{containerPort: 8080, visibility: external}` AND config has `externalServiceType="NodePort"`
- **When:** `Create` is called
- **Then:** Service type is `NodePort`

### TC-I091: Mixed visibility — only non-none ports in Service

- **Requirement:** REQ-K8S-100, REQ-K8S-110
- **Priority:** High
- **Type:** Integration
- **Given:** Container has ports `[{containerPort: 8080, visibility: internal}, {containerPort: 9090, visibility: none}]`
- **When:** `Create` is called
- **Then:** Service is created with only port 8080 AND port 9090 is NOT in the Service

### TC-I092: Mixed internal+external — external promotes Service type

- **Requirement:** REQ-K8S-125
- **Priority:** High
- **Type:** Integration
- **Given:** Container has ports `[{containerPort: 8080, visibility: internal}, {containerPort: 9090, visibility: external}]` AND config has `externalServiceType="LoadBalancer"`
- **When:** `Create` is called
- **Then:** Service type is `LoadBalancer` AND both ports 8080 and 9090 are in the Service

### TC-I093: GET infers internal when Service is ClusterIP

- **Requirement:** REQ-K8S-220
- **Priority:** High
- **Type:** Integration
- **Given:** A Deployment with ports AND a ClusterIP Service exist
- **When:** `Get` is called
- **Then:** `service.name` is populated AND all ports in the Service have `visibility=internal`

### TC-I094: GET infers external when Service is LoadBalancer

- **Requirement:** REQ-K8S-220
- **Priority:** High
- **Type:** Integration
- **Given:** A Deployment with ports AND a LoadBalancer Service exist
- **When:** `Get` is called
- **Then:** `service.name` is populated AND all ports in the Service have `visibility=external`

### TC-I095: GET infers none when no Service exists

- **Requirement:** REQ-K8S-220
- **Priority:** High
- **Type:** Integration
- **Given:** A Deployment with ports exists but no Service
- **When:** `Get` is called
- **Then:** All ports have `visibility=none`

---

## 4 · K8s Store — Conflict & Namespace

> **Suggested Ginkgo structure:** `Describe("K8s Store")` → `Context("conflict detection")` and `Context("namespace")`

### TC-I028: Create returns conflict when Deployment name already exists

- **Requirement:** REQ-K8S-170, REQ-STR-030
- **Priority:** High
- **Type:** Integration
- **Transitively covers:** TC-U026 (Conflict error is distinguishable)
- **Given:** A Deployment named `"web-app"` already exists in the namespace
- **When:** `Create` is called with `metadata.name="web-app"` (different container id)
- **Then:** A conflict error is returned AND the existing Deployment is not modified

### TC-I029: All resources created in the configured namespace

- **Requirement:** REQ-K8S-260
- **Priority:** High
- **Type:** Integration
- **Given:** Config has `namespace="production"` AND container has port with `visibility=internal`
- **When:** `Create` is called
- **Then:** Deployment is in `"production"` namespace AND Service is in `"production"` namespace

### TC-I102: Get returns ConflictError when multiple Deployments share instance ID

- **Requirement:** REQ-K8S-300
- **Priority:** High
- **Type:** Integration
- **Given:** Two Deployments exist with the same `dcm.project/dcm-instance-id` label but different names
- **When:** `Get` is called with that instance ID
- **Then:** A `ConflictError` is returned

### TC-I103: Delete returns ConflictError when multiple Deployments share instance ID

- **Requirement:** REQ-K8S-300
- **Priority:** High
- **Type:** Integration
- **Given:** Two Deployments exist with the same `dcm.project/dcm-instance-id` label but different names
- **When:** `Delete` is called with that instance ID
- **Then:** A `ConflictError` is returned AND neither Deployment is deleted

### TC-I081: Unexpected K8s API error produces internal store error

- **Requirement:** REQ-API-180, REQ-STR-080
- **Priority:** High
- **Type:** Integration
- **Given:** The fake K8s client is configured to return an unexpected error (e.g., 500 Internal Server Error) on Deployment creation
- **When:** `Create` is called
- **Then:** The store returns an internal error (not a conflict or not-found error) AND the error is distinguishable from typed store errors

---

## 5 · K8s Store — Get Operations

> **Suggested Ginkgo structure:** `Describe("K8s Store")` → `Describe("Get")`

### TC-I030: Get returns container with runtime data from Pod and Service

- **Requirement:** REQ-K8S-220, REQ-STR-040
- **Priority:** High
- **Type:** Integration
- **Given:** A container `"abc-123"` exists with a running Pod (IP `"10.0.0.1"`) and a ClusterIP Service
- **When:** `Get` is called with containerId `"abc-123"`
- **Then:**
  - `status` is `RUNNING` (from Pod phase)
  - `network.ip` is `"10.0.0.1"` (from Pod status)
  - `service.name` is populated from Service metadata (the server-generated K8s Service name)
  - `service.clusterIP` is populated from Service spec
  - `service.type` is `"ClusterIP"` from Service spec
  - `service.ports` is populated from Service spec

### TC-I031: Get returns PENDING status when no Pod exists

- **Requirement:** REQ-K8S-240
- **Priority:** High
- **Type:** Integration
- **Given:** A Deployment exists for `"abc-123"` but no Pod has been created
- **When:** `Get` is called
- **Then:** `status` is `PENDING`

### TC-I032: Get returns not-found for non-existent container

- **Requirement:** REQ-STR-040
- **Priority:** High
- **Type:** Integration
- **Transitively covers:** TC-U025 (Not-found error is distinguishable)
- **Given:** No container with id `"xyz-999"` exists
- **When:** `Get` is called with containerId `"xyz-999"`
- **Then:** A not-found error is returned

### TC-I033: Get populates externalIP from LoadBalancer status

- **Requirement:** REQ-K8S-220
- **Priority:** Medium
- **Type:** Integration
- **Given:** A container has a LoadBalancer Service with status.loadBalancer.ingress[0].ip = `"203.0.113.1"`
- **When:** `Get` is called
- **Then:** `service.name` is populated from Service metadata AND `service.externalIP` is `"203.0.113.1"`

### TC-I075: Get populates update_time from Pod condition transition

- **Requirement:** REQ-API-070, REQ-K8S-220
- **Priority:** High
- **Type:** Integration
- **Given:** A container `"abc-123"` has a running Pod with a `Ready` condition whose `lastTransitionTime` is `"2026-02-18T10:00:00Z"`
- **When:** `Get` is called
- **Then:** `update_time` is `"2026-02-18T10:00:00Z"` (derived from the most recent Pod condition transition)

### TC-I076: Get populates update_time from Deployment condition when no Pod

- **Requirement:** REQ-API-070, REQ-K8S-220
- **Priority:** High
- **Type:** Integration
- **Given:** A Deployment exists for `"abc-123"` with condition `Available` having `lastTransitionTime="2026-02-18T09:00:00Z"` AND no Pod exists
- **When:** `Get` is called
- **Then:** `update_time` is `"2026-02-18T09:00:00Z"` (derived from the most recent Deployment condition transition)

### TC-I077: Get returns container without service data when no Service

- **Requirement:** REQ-K8S-220
- **Priority:** Medium
- **Type:** Integration
- **Given:** A container `"abc-123"` has a Deployment and running Pod but no Service
- **When:** `Get` is called
- **Then:** The response contains `status`, `network.ip` from the Pod AND `service` fields are absent or empty (no `clusterIP`, `type`, `ports`)

### 5.1 · Rolling Update Pod Handling

### TC-I107: Returns Running pod when 2 pods exist during rollout

- **Requirement:** REQ-K8S-280
- **Priority:** High
- **Type:** Integration
- **Given:** A Deployment is mid-rollout (UpdatedReplicas < Replicas) with a Running pod and a Pending pod
- **When:** `Get` is called
- **Then:** The response status is `RUNNING` with the Running pod's IP

### TC-I108: Returns newest pod when 2 pods during rollout (both Pending)

- **Requirement:** REQ-K8S-280
- **Priority:** High
- **Type:** Integration
- **Given:** A Deployment is mid-rollout with 2 Pending pods (different creation timestamps)
- **When:** `Get` is called
- **Then:** The response status is `PENDING` (newest pod selected)

### TC-I109: ConflictError when 2 pods but no rollout in progress

- **Requirement:** REQ-K8S-290
- **Priority:** High
- **Type:** Integration
- **Given:** A Deployment has stable status (UpdatedReplicas == Replicas) but 2 pods exist
- **When:** `Get` is called
- **Then:** A `ConflictError` is returned

### TC-I110: ConflictError when 3+ pods regardless of rollout state

- **Requirement:** REQ-K8S-290
- **Priority:** High
- **Type:** Integration
- **Given:** A Deployment is mid-rollout but 3 pods exist
- **When:** `Get` is called
- **Then:** A `ConflictError` is returned

---

## 6 · K8s Store — List Operations

> **Suggested Ginkgo structure:** `Describe("K8s Store")` → `Describe("List")`

### TC-I034: List supports pagination over Deployments

- **Requirement:** REQ-K8S-250, REQ-STR-050
- **Priority:** High
- **Type:** Integration
- **Given:** 5 containers exist in the namespace
- **When:** `List` is called with `max_page_size=2`
- **Then:** At most 2 containers are returned AND `next_page_token` is non-empty

### TC-I035: List with page_token returns subsequent page

- **Requirement:** REQ-STR-050
- **Priority:** High
- **Type:** Integration
- **Given:** A `page_token` obtained from a previous `List` call
- **When:** `List` is called with that `page_token`
- **Then:** The next page of results is returned AND results do not overlap with the first page

### TC-I036: List defaults to page size of 50

- **Requirement:** REQ-STR-060
- **Priority:** Medium
- **Type:** Integration
- **Given:** 75 containers exist AND no `max_page_size` is specified
- **When:** `List` is called
- **Then:** At most 50 containers are returned AND `next_page_token` is non-empty

### TC-I078: List returns error for invalid page_token

- **Requirement:** REQ-STR-050 (see SC-006)
- **Priority:** High
- **Type:** Integration
- **Given:** Containers exist in the namespace
- **When:** `List` is called with `page_token="not-a-valid-token"`
- **Then:** An error is returned indicating the page token is invalid (maps to 400 Bad Request at the handler level)

### TC-I086: List returns error for negative page_token offset

- **Requirement:** REQ-STR-050
- **Priority:** Medium
- **Type:** Integration
- **Given:** Containers exist in the namespace
- **When:** `List` is called with a `page_token` encoding a negative offset
- **Then:** An error is returned indicating the page token is invalid (maps to 400 Bad Request at the handler level)

---

## 7 · K8s Store — Delete Operations

> **Suggested Ginkgo structure:** `Describe("K8s Store")` → `Describe("Delete")`

### TC-I037: Delete removes Deployment and associated Service

- **Requirement:** REQ-K8S-180, REQ-STR-070
- **Priority:** High
- **Type:** Integration
- **Given:** A container `"abc-123"` has both a Deployment and a Service
- **When:** `Delete` is called with containerId `"abc-123"`
- **Then:** The Deployment is deleted AND the Service is deleted AND subsequent `Get("abc-123")` returns not-found

### TC-I038: Delete succeeds when no Service exists

- **Requirement:** REQ-K8S-190
- **Priority:** High
- **Type:** Integration
- **Given:** A container `"abc-123"` has a Deployment but no Service
- **When:** `Delete` is called with containerId `"abc-123"`
- **Then:** The Deployment is deleted AND the operation succeeds without error

### TC-I039: Delete returns not-found for non-existent container

- **Requirement:** REQ-STR-070
- **Priority:** High
- **Type:** Integration
- **Transitively covers:** TC-U025 (Not-found error is distinguishable)
- **Given:** No container with id `"xyz-999"` exists
- **When:** `Delete` is called with containerId `"xyz-999"`
- **Then:** A not-found error is returned

---

## 7.1 · K8s Store — Health Check

> **Suggested Ginkgo structure:** `Describe("K8s Store")` → `Describe("CheckHealth")`

### TC-I116: CheckHealth succeeds with reachable fake client

- **Requirement:** REQ-HLT-050
- **AC:** AC-HLT-020
- **Priority:** High
- **Type:** Integration
- **Transitively covers:** TC-U089 (CheckHealth is part of ContainerRepository satisfied by K8sContainerStore — `var _ store.ContainerRepository = (*K8sContainerStore)(nil)` in test file)
- **Given:** A K8sContainerStore initialized with a fake K8s client
- **When:** `CheckHealth` is called
- **Then:** No error is returned

### TC-I117: CheckHealth returns error when Discovery fails

- **Requirement:** REQ-HLT-050, REQ-HLT-060
- **AC:** AC-HLT-025
- **Priority:** High
- **Type:** Integration
- **Given:** A K8sContainerStore initialized with a fake K8s client configured to return an error on Discovery
- **When:** `CheckHealth` is called
- **Then:** An error is returned

---

## 8 · Monitoring — Informer Setup

> **Suggested Ginkgo structure:** `Describe("Status Monitor")` → `Describe("Informer Setup")`

### TC-I040: Deployment informer watches configured namespace

- **Requirement:** REQ-MON-010
- **Priority:** High
- **Type:** Integration
- **Given:** Config has `namespace="default"`
- **When:** The monitoring subsystem initializes with a fake K8s client
- **Then:** The Deployment informer is created AND watches the `"default"` namespace

### TC-I041: Pod informer watches configured namespace

- **Requirement:** REQ-MON-020
- **Priority:** High
- **Type:** Integration
- **Given:** Config has `namespace="default"`
- **When:** The monitoring subsystem initializes
- **Then:** The Pod informer is created AND watches the `"default"` namespace

### TC-I042: Informers filter resources by DCM labels

- **Requirement:** REQ-MON-030
- **Priority:** High
- **Type:** Integration
- **Transitively covers:** TC-U041 (Missing dcm.project/dcm-instance-id label handled gracefully — non-DCM resources are skipped without error)
- **Given:** The namespace contains both DCM-labeled and non-DCM-labeled Deployments/Pods
- **When:** Informer events are processed
- **Then:** Only resources with labels `dcm.project/managed-by=dcm` AND `dcm.project/dcm-service-type=container` trigger event handlers

### TC-I043: Informer indexer enables lookup by dcm.project/dcm-instance-id

- **Requirement:** REQ-MON-040
- **Priority:** High
- **Type:** Integration
- **Transitively covers:** TC-U040 (Instance ID extracted from dcm.project/dcm-instance-id label), TC-U042 (Indexer function returns dcm.project/dcm-instance-id value)
- **Given:** The informer cache contains resources with `dcm.project/dcm-instance-id="abc-123"`
- **When:** An index lookup for `"abc-123"` is performed
- **Then:** The correct resource(s) are returned

---

## 9 · Monitoring — Status Reconciliation

> **Suggested Ginkgo structure:** `Describe("Status Monitor")` → `Describe("Reconciliation")`

### TC-I044: Pod update triggers status reconciliation with Pod priority

- **Requirement:** REQ-MON-050
- **Priority:** High
- **Type:** Integration
- **Transitively covers:** TC-U040 (Instance ID extracted from label), TC-U029 (Pod phase Running → RUNNING)
- **Given:** A Deployment and Pod exist for instance `"abc-123"` AND Pod phase changes to `Running`
- **When:** The Pod informer fires an update event
- **Then:** Status is reconciled to `RUNNING` AND a CloudEvent is constructed with status `RUNNING`

### TC-I045: Deployment update without Pod falls back to Deployment status

- **Requirement:** REQ-MON-060
- **Priority:** High
- **Type:** Integration
- **Given:** A Deployment exists for instance `"abc-123"` with `Available=False` AND no Pod exists
- **When:** The Deployment informer fires an update event
- **Then:** Status is reconciled to `PENDING`

### TC-I046: Deletion event produces DELETED status

- **Requirement:** REQ-MON-070
- **Priority:** High
- **Type:** Integration
- **Given:** Instance `"abc-123"` was previously tracked AND its Deployment is deleted
- **When:** The Deployment informer fires a delete event
- **Then:** Status is reconciled to `DELETED` AND a CloudEvent with status `DELETED` is published

### TC-I062: Pod phase Pending produces PENDING status

- **Requirement:** REQ-K8S-230, REQ-MON-080
- **Priority:** High
- **Type:** Integration
- **Transitively covers:** TC-U029 (Pod phase Pending → PENDING)
- **Given:** A Deployment and Pod exist for instance `"abc-123"` AND Pod phase is `Pending`
- **When:** The Pod informer fires an update event
- **Then:** Status is reconciled to `PENDING` AND a CloudEvent is constructed with status `PENDING`

### TC-I063: Pod phase Failed produces FAILED status

- **Requirement:** REQ-K8S-230, REQ-MON-080
- **Priority:** High
- **Type:** Integration
- **Transitively covers:** TC-U029 (Pod phase Failed → FAILED)
- **Given:** A Deployment and Pod exist for instance `"abc-123"` AND Pod phase is `Failed`
- **When:** The Pod informer fires an update event
- **Then:** Status is reconciled to `FAILED` AND a CloudEvent is constructed with status `FAILED`

### TC-I064: Pod phase Unknown produces UNKNOWN status

- **Requirement:** REQ-K8S-230, REQ-MON-080
- **Priority:** High
- **Type:** Integration
- **Transitively covers:** TC-U029 (Pod phase Unknown → UNKNOWN)
- **Given:** A Deployment and Pod exist for instance `"abc-123"` AND Pod phase is `Unknown`
- **When:** The Pod informer fires an update event
- **Then:** Status is reconciled to `UNKNOWN` AND a CloudEvent is constructed with status `UNKNOWN`

### TC-I065: Pod phase Succeeded produces no event

- **Requirement:** REQ-K8S-230, REQ-MON-080
- **Priority:** High
- **Type:** Integration
- **Transitively covers:** TC-U030 (Pod Succeeded phase is explicitly ignored)
- **Given:** A Deployment and Pod exist for instance `"abc-123"` AND Pod phase transitions to `Succeeded`
- **When:** The Pod informer fires an update event
- **Then:** No CloudEvent is published AND the instance status is not updated

### TC-I113: Deploy deleted while Pod still exists uses Pod status

- **Requirement:** REQ-MON-050, REQ-MON-070
- **Priority:** High
- **Type:** Integration
- **Given:** A Deployment and a Running Pod exist for instance `"abc-123"`
- **When:** The Deployment is deleted (Pod remains)
- **Then:** The last published status for `"abc-123"` is `RUNNING` (not `DELETED`) because the Pod takes precedence per REQ-MON-050

### TC-I114: Pod deleted while remaining Pods exist uses worst remaining Pod

- **Requirement:** REQ-MON-050
- **Priority:** High
- **Type:** Integration
- **Given:** A Deployment and two Pods exist for instance `"abc-123"`: one `Pending` (`pod-a`) and one `Running` (`pod-b`)
- **When:** The Pending Pod (`pod-a`) is deleted
- **Then:** The last published status for `"abc-123"` is `RUNNING` (from the surviving Pod), not `PENDING` (deploy-only fallback)

---

## 10 · Monitoring — NATS Publishing

> **Suggested Ginkgo structure:** `Describe("Status Monitor")` → `Describe("NATS Publishing")`

### TC-I047: Status event published to correct NATS subject with valid CloudEvent

- **Requirement:** REQ-MON-100, REQ-MON-095
- **Priority:** High
- **Type:** Integration
- **Transitively covers:** TC-U036 (CloudEvent has correct v1.0 structure)
- **Given:** Provider name is `"k8s-sp"` AND a status change occurs for instance `"abc-123"` with status `RUNNING`
- **When:** The CloudEvent is published to NATS
- **Then:** The NATS subject is `"dcm.container"` AND the message is a valid CloudEvent with:
  - `source`: `"dcm/providers/k8s-sp"`
  - `type`: `"dcm.status.container"`
  - `subject`: `"dcm.container"`
  - `datacontenttype`: `"application/json"`
  - `data.id`: `"abc-123"`
  - `data.status`: `"RUNNING"`

### TC-I048: FAILED event published with failure reason and instance ID

- **Requirement:** REQ-MON-150, REQ-MON-095
- **Priority:** High
- **Type:** Integration
- **Transitively covers:** TC-U037 (FAILED CloudEvent includes failure reason in message)
- **Given:** A Pod fails with container status reason `"CrashLoopBackOff"` for instance `"abc-123"`
- **When:** The status event is published via NATS
- **Then:** The CloudEvent `data.id` is `"abc-123"` AND `data.status` is `"FAILED"` AND `data.message` includes `"CrashLoopBackOff"`

---

## 11 · Monitoring — Lifecycle, Sync & Debounce

> **Suggested Ginkgo structure:** `Describe("Status Monitor")` → `Describe("Lifecycle")`

### TC-I049: Resource watchers start after HTTP server is ready

- **Requirement:** REQ-MON-130
- **Priority:** High
- **Type:** Integration
- **Given:** The SP startup sequence
- **When:** Components are initialized
- **Then:** The HTTP server begins accepting connections BEFORE resource watchers start watching

### TC-I050: Resource watchers stop during graceful shutdown

- **Requirement:** REQ-MON-131
- **Priority:** High
- **Type:** Integration
- **Given:** Resource watchers are running
- **When:** A shutdown signal is received
- **Then:** Resource watchers are stopped AND no background task leaks occur

### TC-I051: Cache resync triggers re-evaluation for all cached resources

- **Requirement:** REQ-MON-140
- **Priority:** Medium
- **Type:** Integration
- **Given:** Resource watchers are running with a short resync period (e.g., 1 second for testing)
- **When:** The resync period elapses
- **Then:** Status reconciliation is re-evaluated for every resource currently in the cache

### TC-I052: Initial status sync publishes events for all existing resources

- **Requirement:** REQ-MON-145
- **Priority:** High
- **Type:** Integration
- **Given:** 3 DCM-managed resources exist in the cluster before the SP starts
- **When:** SP starts AND the resource cache completes initial synchronization
- **Then:** 3 CloudEvents are published (one per resource) AND debounce logic applies

### TC-I066: Rapid status changes within debounce window publish only the last event

- **Requirement:** REQ-MON-110
- **Priority:** High
- **Type:** Integration
- **Transitively covers:** TC-U038 (Only last event within debounce window is published)
- **Given:** The monitoring subsystem is running with debounce interval of 100ms (shortened for testing) AND a Deployment and Pod exist for instance `"abc-123"`
- **When:** Pod phase changes rapidly within 100ms: `Pending` → `Running` → `Failed`
- **Then:** Only one CloudEvent is published to NATS AND its status is `FAILED`

### TC-I067: Status changes after debounce window are published separately

- **Requirement:** REQ-MON-110
- **Priority:** Medium
- **Type:** Integration
- **Transitively covers:** TC-U039 (Events after debounce window are published separately)
- **Given:** The monitoring subsystem is running with debounce interval of 100ms AND a Deployment and Pod exist for instance `"abc-123"`
- **When:** Pod phase changes to `Running`, the debounce window elapses fully, then Pod phase changes to `Failed`
- **Then:** Two separate CloudEvents are published to NATS: first with status `RUNNING`, then with status `FAILED`

### TC-I115: Debouncing operates independently per instance ID

- **Requirement:** REQ-MON-110
- **AC:** AC-MON-101
- **Priority:** High
- **Type:** Integration
- **Given:** A debouncer with interval of 100ms
- **When:** Rapid events are submitted for instance `"instance-a"` (PENDING then RUNNING) AND a single event is submitted for instance `"instance-b"` (FAILED) within the same window
- **Then:** Two events are published total: one for `"instance-a"` with status `RUNNING` (coalesced) AND one for `"instance-b"` with status `FAILED` (independent)

### TC-I099: Watchers reconnect after API server interruption

- **Requirement:** REQ-MON-160
- **Priority:** High
- **Type:** Integration
- **Status:** Implemented
- **Given:** Resource watchers are running and connected to the API server
- **When:** The API server connection is interrupted and then restored
- **Then:** Resource watchers MUST automatically reconnect and resume processing events

### TC-I100: Publisher retries on transient NATS failure

- **Requirement:** REQ-MON-180
- **Priority:** High
- **Type:** Integration
- **Status:** Implemented
- **Given:** A status event needs to be published
- **When:** NATS returns a transient failure on the first attempt
- **Then:** The publisher MUST retry with increasing intervals (exponential backoff)

### TC-I101: SP continues serving when NATS is unavailable

- **Requirement:** REQ-MON-190
- **Priority:** High
- **Type:** Integration
- **Status:** Implemented
- **Given:** NATS is unavailable
- **When:** The SP attempts to publish a status event
- **Then:** The failure MUST be logged at ERROR level AND the SP MUST continue serving HTTP requests

---

## 12 · DCM Registration

> **Suggested Ginkgo structure:** `Describe("DCM Registration")`
> Tests use an `httptest.Server` as a mock DCM registry.

### TC-I053: SP registers with DCM on startup

- **Requirement:** REQ-REG-010
- **Priority:** High
- **Type:** Integration
- **Given:** A mock DCM registration server is running
- **When:** The SP starts with valid registration configuration
- **Then:** A `POST` request is received at `{dcm.registrationUrl}/providers`

### TC-I054: Registration payload contains correct fields

- **Requirement:** REQ-REG-020
- **Priority:** High
- **Type:** Integration
- **Transitively covers:** TC-U043 (Payload contains all configured fields), TC-U044 (Payload includes region and zone metadata)
- **Given:** Provider config: `name="k8s-sp"`, `display_name="K8s SP"`, `endpoint="https://sp.example.com"`, `region="us-east-1"`, `zone="us-east-1a"`
- **When:** Registration is sent to the mock server
- **Then:** Request body contains:
  - `name: "k8s-sp"`
  - `service_type: "container"`
  - `schema_version: "v1alpha1"`
  - `display_name: "K8s SP"`
  - `endpoint: "https://sp.example.com/api/v1alpha1/containers"`
  - `operations: ["CREATE", "DELETE", "READ"]`
  - `metadata.region_code: "us-east-1"`
  - `metadata.zone: "us-east-1a"`

### TC-I055: Registration does not block server startup

- **Requirement:** REQ-REG-030
- **Priority:** High
- **Type:** Integration
- **Given:** The mock DCM registration server delays responses by 5 seconds
- **When:** The SP starts
- **Then:** The HTTP server accepts requests within 1 second of startup AND registration completes later in the background

### TC-I056: Registration retries with exponential backoff

- **Requirement:** REQ-REG-040
- **Priority:** High
- **Type:** Integration
- **Given:** The mock DCM registration server returns 500 errors for the first 3 attempts
- **When:** Registration is attempted
- **Then:** At least 4 requests are made AND the interval between requests increases AND registration eventually succeeds when the server returns 200

### TC-I057: Registration failure is logged without causing SP exit

- **Requirement:** REQ-REG-050
- **Priority:** High
- **Type:** Integration
- **Given:** The mock DCM registration server is permanently unreachable
- **When:** Registration attempts fail repeatedly
- **Then:** Error log entries are produced AND the SP continues running AND HTTP requests are served normally

### TC-I058: Re-registration sends idempotent payload

- **Requirement:** REQ-REG-060
- **Priority:** Medium
- **Type:** Integration
- **Given:** The SP was previously registered (mock server received a registration)
- **When:** The SP "restarts" and re-registers
- **Then:** The same registration payload is sent to the mock server (allowing the registry to deduplicate)

### TC-I059: Registration uses DCM client library

- **Requirement:** REQ-REG-070
- **Priority:** Medium
- **Type:** Integration
- **Given:** The registration subsystem is initialized
- **When:** Registration is performed
- **Then:** The call goes through `github.com/dcm-project/service-provider-api/pkg/registration/client` (verified by the mock server receiving the expected request format from the library)

### TC-I068: Registration omits optional fields when not configured

- **Requirement:** REQ-REG-020
- **Priority:** Medium
- **Type:** Integration
- **Transitively covers:** TC-U045 (Payload omits metadata when region and zone not configured), TC-U064 (Payload omits display_name when not configured)
- **Given:** Provider config: `name="k8s-sp"`, `endpoint="https://sp.example.com"` with NO `display_name`, `region`, or `zone` configured
- **When:** Registration is sent to the mock server
- **Then:** Request body contains `name`, `service_type`, `endpoint`, `operations` AND `display_name`, `metadata.region_code`, and `metadata.zone` are absent from the payload

### TC-I083: Repeated registration start requests produce only one registration attempt

- **Requirement:** REQ-REG-030
- **Priority:** High
- **Type:** Integration
- **Given:** A registrar with valid configuration and a mock DCM server that counts incoming requests
- **When:** The registration process is initiated multiple times on the same registrar
- **Then:** Exactly one registration attempt is made to the DCM server

### TC-I084: Registration process terminates cleanly on shutdown and releases resources

- **Requirement:** REQ-REG-030
- **Priority:** High
- **Type:** Integration
- **Given:** A registrar whose registration process is running
- **When:** A shutdown signal is issued
- **Then:** The registration process completes within a reasonable timeout, signals its termination, and releases all associated resources

### TC-I080: Registration backoff has maximum interval cap

- **Requirement:** REQ-REG-040
- **Priority:** Medium
- **Type:** Integration
- **Given:** The mock DCM registration server is permanently unreachable AND the registration client uses a short initial backoff (e.g., 10ms) with a capped maximum interval (e.g., 200ms)
- **When:** Registration attempts fail repeatedly (e.g., 10+ attempts)
- **Then:** The interval between consecutive attempts never exceeds the configured maximum cap AND the backoff pattern is exponential up to the cap

### TC-I087: Done() channel closes after successful registration

- **Requirement:** REQ-REG-030
- **Priority:** Medium
- **Type:** Integration
- **Given:** A registrar with valid configuration and a mock DCM server that returns 200 OK
- **When:** Registration completes successfully
- **Then:** The `Done()` channel MUST close, signaling that the registration process has finished

### TC-I104: Registration stops retrying on 4xx client error

- **Requirement:** REQ-REG-040
- **AC:** AC-REG-045
- **Priority:** High
- **Type:** Integration
- **Given:** A mock DCM server that returns 400 Bad Request
- **When:** A registration attempt receives this response
- **Then:** The registrar MUST NOT retry (no further requests after the first)
- **And** the error MUST be logged at ERROR level
- **And** the `Done()` channel MUST close

---

## 13 · E2E Placeholders

> These tests require a real Kubernetes cluster and cannot run in standard CI.
> They are included as placeholders for E2E test suites.

### TC-I060: Kubeconfig authentication

- **Requirement:** REQ-K8S-200
- **Priority:** High
- **Type:** E2E (placeholder)
- **Given:** `SP_K8S_KUBECONFIG` points to a valid kubeconfig file for a real cluster
- **When:** The SP initializes the K8s client
- **Then:** The client authenticates using kubeconfig credentials AND can list Deployments in the configured namespace
- **Note:** Requires a real Kubernetes cluster. Skip in CI. Run manually or in E2E pipeline.

### TC-I061: In-cluster service account authentication

- **Requirement:** REQ-K8S-210
- **Priority:** High
- **Type:** E2E (placeholder)
- **Given:** `SP_K8S_KUBECONFIG` is not set AND the SP runs inside a Kubernetes Pod with a ServiceAccount
- **When:** The SP initializes the K8s client
- **Then:** The client authenticates using the mounted service account token AND can list Deployments in the configured namespace
- **Note:** Requires deploying the SP to a real cluster. Skip in CI. Run in E2E pipeline.

---

## Coverage Matrix

| Requirement    | Test Cases                          | Status  |
|----------------|-------------------------------------|---------|
| REQ-HTTP-010   | TC-I001, TC-I082, TC-I085           | Covered |
| REQ-HTTP-020   | TC-I002, TC-I003                    | Covered |
| REQ-HTTP-030   | TC-I004, TC-I079                    | Covered |
| REQ-HTTP-040   | TC-I005                             | Covered |
| REQ-HTTP-060   | TC-I096, TC-I097                    | Covered |
| REQ-HTTP-070   | TC-I102, TC-I104, TC-I105, TC-I106  | Covered |
| REQ-HTTP-080   | TC-I006, TC-I007                    | Covered |
| REQ-HTTP-090   | TC-I008                             | Covered |
| REQ-HTTP-110   | TC-I098                             | Covered |
| REQ-HLT-010    | TC-I103                             | Covered |
| REQ-HLT-050    | TC-I116, TC-I117                    | Covered |
| REQ-HLT-060    | TC-I117, TC-I118                    | Covered |
| REQ-API-070    | TC-I075, TC-I076                    | Covered |
| REQ-API-151    | TC-I037                             | Covered |
| REQ-API-180    | TC-I081                             | Covered |
| REQ-API-210    | TC-I112                             | Covered |
| REQ-STR-020    | TC-I009                             | Covered |
| REQ-STR-030    | TC-I028                             | Covered |
| REQ-STR-040    | TC-I030, TC-I032                    | Covered |
| REQ-STR-050    | TC-I034, TC-I035, TC-I078, TC-I086  | Covered |
| REQ-STR-060    | TC-I036                             | Covered |
| REQ-STR-070    | TC-I037, TC-I039                    | Covered |
| REQ-STR-080    | TC-I081                             | Covered |
| REQ-K8S-010    | TC-I009                             | Covered |
| REQ-K8S-020    | TC-I010, TC-I070, TC-I071           | Covered |
| REQ-K8S-030    | TC-I011                             | Covered |
| REQ-K8S-040    | TC-I012                             | Covered |
| REQ-K8S-050    | TC-I013                             | Covered |
| REQ-K8S-060    | TC-I014, TC-I018                    | Covered |
| REQ-K8S-070    | TC-I015, TC-I018                    | Covered |
| REQ-K8S-080    | TC-I016, TC-I018                    | Covered |
| REQ-K8S-090    | TC-I017, TC-I018                    | Covered |
| REQ-K8S-100    | TC-I019, TC-I091, TC-I092           | Covered |
| REQ-K8S-110    | TC-I022, TC-I091                    | Covered |
| REQ-K8S-120    | TC-I023                             | Covered |
| REQ-K8S-125    | TC-I024, TC-I074, TC-I092           | Covered |
| REQ-K8S-150    | TC-I025, TC-I026                    | Covered |
| REQ-K8S-155    | TC-I111                             | Covered |
| REQ-K8S-170    | TC-I028, TC-I069                    | Covered |
| REQ-K8S-180    | TC-I037                             | Covered |
| REQ-K8S-190    | TC-I038                             | Covered |
| REQ-K8S-200    | TC-I060 (E2E placeholder)           | Covered |
| REQ-K8S-210    | TC-I061 (E2E placeholder)           | Covered |
| REQ-K8S-220    | TC-I030, TC-I033, TC-I075, TC-I076, TC-I077, TC-I093, TC-I094, TC-I095 | Covered |
| REQ-K8S-230    | TC-I030 (implicit), TC-I062–I065    | Covered |
| REQ-K8S-240    | TC-I031                             | Covered |
| REQ-K8S-250    | TC-I034                             | Covered |
| REQ-K8S-260    | TC-I029                             | Covered |
| REQ-K8S-270    | TC-I027                             | Covered |
| REQ-K8S-280    | TC-I107, TC-I108                    | Covered |
| REQ-K8S-290    | TC-I109, TC-I110                    | Covered |
| REQ-K8S-300    | TC-I102, TC-I103                    | Covered |
| REQ-MON-010    | TC-I040                             | Covered |
| REQ-MON-020    | TC-I041                             | Covered |
| REQ-MON-030    | TC-I042                             | Covered |
| REQ-MON-040    | TC-I043                             | Covered |
| REQ-MON-050    | TC-I044, TC-I113, TC-I114           | Covered |
| REQ-MON-060    | TC-I045                             | Covered |
| REQ-MON-070    | TC-I046, TC-I113                    | Covered |
| REQ-MON-080    | TC-I062–I065                        | Covered |
| REQ-MON-095    | TC-I047, TC-I048                    | Covered |
| REQ-MON-100    | TC-I047                             | Covered |
| REQ-MON-110    | TC-I066, TC-I067, TC-I115           | Covered |
| REQ-MON-130    | TC-I049                             | Covered |
| REQ-MON-131    | TC-I050                             | Covered |
| REQ-MON-140    | TC-I051                             | Covered |
| REQ-MON-145    | TC-I052                             | Covered |
| REQ-MON-150    | TC-I048                             | Covered |
| REQ-MON-160    | TC-I099                             | Covered |
| REQ-MON-180    | TC-I100                             | Covered |
| REQ-MON-190    | TC-I101                             | Covered |
| REQ-REG-010    | TC-I053                             | Covered |
| REQ-REG-020    | TC-I054, TC-I068                    | Covered |
| REQ-REG-030    | TC-I055, TC-I083, TC-I084, TC-I087  | Covered |
| REQ-REG-031    | TC-I055                             | Covered |
| REQ-REG-040    | TC-I056, TC-I080, TC-I104           | Covered |
| REQ-REG-050    | TC-I057                             | Covered |
| REQ-REG-051    | TC-I057                             | Covered |
| REQ-REG-060    | TC-I058                             | Covered |
| REQ-REG-070    | TC-I059                             | Covered |
| REQ-XC-ID-010  | TC-I010 (dcm.project/dcm-instance-id label), TC-I009 (metadata.name as generateName prefix) | Covered |
| REQ-XC-ID-020  | TC-I028, TC-I069                    | Covered |
| REQ-XC-LBL-010 | TC-I010, TC-I027, TC-I070           | Covered |
| REQ-XC-ERR-010 | TC-I008                             | Covered |
| REQ-XC-ERR-020 | TC-I008                             | Covered |
| REQ-XC-LOG-010 | TC-I006, TC-I007                    | Covered |
| REQ-XC-LOG-020 | TC-I006, TC-I007 (INFO), TC-I057 (ERROR) | Covered |

**Total:** 99 integration test cases (including 2 E2E placeholders, 3 pending
monitoring implementation) covering 79 requirements at integration level.

> Requirements not listed above (REQ-HTTP-050–070, REQ-HLT-010–040,
> REQ-API-010–060, REQ-API-080–160, REQ-API-170, REQ-STR-010, REQ-MON-090,
> REQ-MON-120, REQ-XC-CFG-010, REQ-XC-CFG-030) are covered in the unit test plan.

---

## Utility Transitive Coverage Summary

The following utility TC-IDs from the unit test plan are transitively exercised
by integration tests in this plan:

| Utility TC | Description | Referenced by |
|---|---|---|
| TC-U024 | ContainerRepository interface satisfied | TC-I009 |
| TC-U025 | Not-found error distinguishable | TC-I032, TC-I039 |
| TC-U026 | Conflict error distinguishable | TC-I028 |
| TC-U027 | CPU → K8s resource quantities | TC-I012 |
| TC-U028 | Memory unit conversion | TC-I013 |
| TC-U029 | Pod phase → DCM status | TC-I044, TC-I062, TC-I063, TC-I064 |
| TC-U030 | Pod Succeeded ignored | TC-I065 |
| TC-U036 | CloudEvent v1.0 structure (source, type, data.id) | TC-I047 |
| TC-U037 | FAILED event includes reason and instance ID | TC-I048 |
| TC-U038 | Debounce within window | TC-I066 |
| TC-U039 | Events after debounce window | TC-I067 |
| TC-U040 | Instance ID extraction | TC-I043, TC-I044 |
| TC-U041 | Missing label handled | TC-I042 |
| TC-U042 | Indexer function | TC-I043 |
| TC-U043 | Registration required fields | TC-I054 |
| TC-U044 | Region/zone metadata | TC-I054 |
| TC-U045 | Metadata omitted when unconfigured | TC-I068 |
| TC-U049 | DCM label collision rejected | TC-I071 |
| TC-U057 | max_page_size boundary enforcement | TC-I008 |
| TC-U058 | Invalid containerId parameter | TC-I008 |
| TC-U089 | CheckHealth in ContainerRepository satisfied by K8sContainerStore | TC-I116 |

---

## Notes

- **Fake K8s client limitations:** `client-go/kubernetes/fake` has limited support for `continue` tokens in list operations. TC-I034/TC-I035 may need workarounds or may verify behavior at a higher level. If the fake client does not support pagination, note this as a known limitation and defer to E2E testing.
- **NATS testing:** TC-I047/TC-I048 can use either an embedded NATS server (`nats-server` in-process) or a mock NATS connection. The embedded approach provides higher fidelity.
- **Graceful shutdown tests:** TC-I004/TC-I005 should launch the server in a goroutine, send a signal via `syscall.Kill(pid, signal)`, and verify behavior. Use a short shutdown timeout (e.g., 2 seconds) for fast test execution.
- **Registration backoff:** TC-I056 should use a short initial backoff (e.g., 10ms) for testing to avoid slow tests. The exponential pattern is what matters, not the actual intervals.
- **Resync period:** TC-I051 should use a 1-second resync period to avoid long waits.
- **Debounce testing:** TC-I066/TC-I067 should use a shortened debounce interval (e.g., 100ms) to avoid slow tests. The behaviour is what matters — rapid changes within the window produce one event, changes after the window produce separate events.
- **Build tag:** Integration tests that require external dependencies (NATS, longer execution) should use a `//go:build integration` build tag so they can be run separately from unit tests via `go test -tags integration ./...`.
- **REQ-REG-070 verification:** TC-I059 verifies library usage indirectly by checking the request format matches what the DCM registration client library produces. Direct import verification is a code review concern.
