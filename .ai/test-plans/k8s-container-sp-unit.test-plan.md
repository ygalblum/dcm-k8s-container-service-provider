# Test Plan: K8s Container SP — Unit Tests

## Overview

- **Related Spec:** .ai/specs/k8s-container-sp.spec.md
- **Related Requirements:** REQ-HTTP-050, REQ-HTTP-091, REQ-HTTP-090, REQ-HLT-010–040, REQ-API-010–180, REQ-STR-010, REQ-STR-080, REQ-K8S-040, REQ-K8S-050, REQ-K8S-230, REQ-MON-040–095, REQ-MON-110–120, REQ-MON-150, REQ-MON-170, REQ-REG-020, REQ-XC-ID-010–020, REQ-XC-ERR-010–040, REQ-XC-CFG-010–030
- **Framework:** Ginkgo v2 + Gomega
- **Created:** 2026-02-17
- **Last Updated:** 2026-04-29 (updated REQ-XC-ID-010 coverage for generateName)

Unit tests verify individual components in isolation. All external dependencies
(ContainerRepository, K8s client, NATS, HTTP server) are replaced with mocks,
fakes, or test doubles. Tests use `httptest.NewRecorder` for handler tests and
direct function calls for pure logic.

### Utility Test Case Approach

Utility and helper functions (resource conversion, error types, event
construction, debounce, indexer functions, registration payload builders) are
**not** tested in dedicated test classes. Instead:

- Each utility behaviour retains a **TC-ID** for requirements traceability.
- The TC-ID is **referenced** in the higher-level behavioural test(s) that
  exercise the utility transitively.
- All utility TC-IDs, their descriptions, and cross-references are collected in
  the [Utility Test Case Index](#utility-test-case-index) at the end of this
  document.

---

## 1 · Configuration

> **Suggested Ginkgo structure:** `Describe("Configuration")`

### TC-U002: Load configuration from environment variables

- **Requirement:** REQ-HTTP-050
- **Priority:** High
- **Type:** Unit
- **Given:** `SP_SERVER_ADDRESS=":9090"` and `SP_SERVER_SHUTDOWN_TIMEOUT="30s"` are set
- **When:** Config is loaded
- **Then:** The loaded config has `server.address = ":9090"` and `shutdownTimeout = 30s`

### TC-U004: Default values applied when no config specified

- **Requirement:** REQ-HTTP-050
- **Priority:** Medium
- **Type:** Unit
- **Given:** No config file is specified AND no environment variables are set
- **When:** Config is loaded
- **Then:** `server.address` defaults to `":8080"` AND `shutdownTimeout` defaults to `15s` (note: `externalServiceType` has no default — validated as required by TC-U082)

---

## 2 · (Reserved — section removed, TCs moved to Section 3)

---

## 3 · Container API Handlers

> **Suggested Ginkgo structure:** `Describe("Container API Handlers")` with
> nested `Describe` per operation and `Context` per scenario. All tests use a
> mocked `ContainerRepository`.

### TC-U005: Returns 200 with correct response fields when healthy

- **Requirement:** REQ-HLT-010, REQ-HLT-020, REQ-HLT-050
- **Priority:** High
- **Type:** Unit
- **Transitively covers:** TC-U007 (GetHealth uses only `startTime`, `version`, and `store.CheckHealth` — never touches CRUD methods; REQ-HLT-040 satisfied)
- **Given:** A `Handler` is initialized with a known start time, version `"2.3.4"`, and a mock `ContainerRepository` whose `CheckHealth` returns `nil`
- **When:** `GetHealth` is called on the `StrictServerInterface`
- **Then:**
  - Response is `GetHealth200JSONResponse`
  - `status` is `"healthy"`
  - `type` is `"k8s-container-service-provider.dcm.io/health"`
  - `path` is `"health"`
  - `version` is `"2.3.4"`
  - `uptime` is an integer `>= 0`

### TC-U006: Uptime increases over time

- **Requirement:** REQ-HLT-020
- **Priority:** Medium
- **Type:** Unit
- **Given:** A `Handler` was initialized with a start time 60 seconds in the past and a mock `ContainerRepository` whose `CheckHealth` returns `nil`
- **When:** `GetHealth` is called on the `StrictServerInterface`
- **Then:** `uptime` is `>= 60`

### TC-U087: GetHealth returns "unhealthy" when health check fails

- **Requirement:** REQ-HLT-020, REQ-HLT-060
- **Priority:** High
- **Type:** Unit
- **Given:** A `Handler` is initialized with a mock `ContainerRepository` whose `CheckHealth` returns an error
- **When:** `GetHealth` is called on the `StrictServerInterface`
- **Then:**
  - Response is `GetHealth200JSONResponse`
  - `status` is `"unhealthy"`

### TC-U088: GetHealth returns all fields when unhealthy

- **Requirement:** REQ-HLT-060
- **Priority:** High
- **Type:** Unit
- **Given:** A `Handler` is initialized with version `"2.3.4"` and a mock `ContainerRepository` whose `CheckHealth` returns an error
- **When:** `GetHealth` is called on the `StrictServerInterface`
- **Then:**
  - `status` is `"unhealthy"`
  - `type` is `"k8s-container-service-provider.dcm.io/health"`
  - `path` is `"health"`
  - `version` is `"2.3.4"`
  - `uptime` is an integer `>= 0`

### TC-U089: CheckHealth is part of ContainerRepository satisfied by K8sContainerStore

- **Requirement:** REQ-HLT-070
- **Priority:** High
- **Type:** Unit (compile-time assertion)
- **Given:** The `ContainerRepository` interface includes `CheckHealth(ctx context.Context) error`
- **When:** A compile-time type assertion of `K8sContainerStore` to `ContainerRepository` is performed
- **Then:** The assertion compiles and succeeds (covered by existing TC-U024 assertion)

### TC-U009: CreateContainer returns 201 with populated read-only fields

- **Requirement:** REQ-API-020, REQ-API-060, REQ-API-070
- **Priority:** High
- **Type:** Unit
- **Transitively covers:** TC-U008 (Handler implements StrictServerInterface — verified by compile-time assertion in test file)
- **Given:** A valid request body with `metadata.name="my-app"`, `image.reference="nginx:latest"`, `serviceType="container"`, CPU and memory resources
- **When:** `POST /api/v1alpha1/containers` is handled (mock repository returns success)
- **Then:**
  - HTTP status is `201`
  - Response `id` is non-empty
  - Response `path` is `"containers/{id}"`
  - Response `status` is `"PENDING"`
  - Response `create_time` is a valid timestamp close to now
  - Response `update_time` equals `create_time`
  - Response `metadata.namespace` matches the configured namespace

### TC-U010: CreateContainer generates UUID when no id query param

- **Requirement:** REQ-API-030
- **Priority:** High
- **Type:** Unit
- **Given:** A valid container request body
- **When:** `POST /api/v1alpha1/containers` is called without `?id=`
- **Then:** Response `id` is a valid UUID (matches UUID format)

### TC-U011: CreateContainer uses client-specified id

- **Requirement:** REQ-API-040
- **Priority:** High
- **Type:** Unit
- **Given:** A valid container request body
- **When:** `POST /api/v1alpha1/containers?id=my-web-app` is called
- **Then:** Response `id` is `"my-web-app"`

### TC-U012: CreateContainer rejects invalid client IDs

- **Requirement:** REQ-API-050
- **Priority:** High
- **Type:** Unit (table-driven)
- **Given:** Invalid IDs that violate pattern `^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`
- **When:** `POST /api/v1alpha1/containers?id={invalid}` is called for each:
  - `"UPPERCASE"` (uppercase letters)
  - `"-leading-dash"` (starts with dash)
  - `"trailing-"` (ends with dash)
  - `"has_underscore"` (underscore)
  - `"a"` + 63 chars (exceeds 63 character limit)
- **Then:** Each returns HTTP `400` with an RFC 7807 error body containing type `INVALID_ARGUMENT`

### TC-U013: CreateContainer returns 409 on name conflict

- **Requirement:** REQ-API-080
- **Priority:** High
- **Type:** Unit
- **Transitively covers:** TC-U026 (Conflict error is distinguishable — mock repository returns a typed conflict error that the handler maps to 409)
- **Given:** Mock repository returns a conflict error for `metadata.name="existing-app"`
- **When:** `POST` with `metadata.name="existing-app"` is called
- **Then:** HTTP status is `409` AND body is RFC 7807 error with type `ALREADY_EXISTS`

### TC-U014: CreateContainer validates request body

- **Requirement:** REQ-API-090, REQ-HTTP-090 (OpenAPI contract enforcement)
- **Priority:** High
- **Type:** Unit (table-driven)
- **Transitively covers:** TC-U052 (invalid serviceType), TC-U053 (invalid metadata.name format), TC-U054 (invalid memory format), TC-U055 (CPU below minimum), TC-U056 (port out of range)
- **Given:** Request bodies each missing a required field or containing an invalid value
- **When:** `POST` is called for each:
  - Missing `image` entirely
  - Missing `image.reference`
  - Missing `metadata` entirely
  - Missing `metadata.name`
  - Missing `resources` entirely
  - Missing `resources.cpu`
  - Missing `resources.cpu.min` or `resources.cpu.max`
  - Missing `resources.memory`
  - Missing `resources.memory.min` or `resources.memory.max`
  - Missing `serviceType`
  - Invalid `serviceType` value (e.g., `"not-a-service-type"`) (TC-U052)
  - Invalid `metadata.name` format (e.g., `"Invalid_Name!"`) (TC-U053)
  - Invalid memory format (e.g., `"10XB"`) (TC-U054)
  - CPU value below minimum (e.g., `cpu.min=0`) (TC-U055)
  - Port out of range (e.g., `containerPort=99999`) (TC-U056)
- **Then:** Each returns HTTP `400` with RFC 7807 error body containing type `INVALID_ARGUMENT`

### TC-U015: ListContainers returns 200 with containers

- **Requirement:** REQ-API-100
- **Priority:** High
- **Type:** Unit
- **Given:** Mock repository returns a list of 3 containers
- **When:** `GET /api/v1alpha1/containers` is called
- **Then:** HTTP status is `200` AND body has `containers` array with 3 items conforming to Container schema

### TC-U016: ListContainers supports pagination parameters

- **Requirement:** REQ-API-110
- **Priority:** High
- **Type:** Unit
- **Given:** Mock repository returns 10 containers and a `next_page_token` when `max_page_size=10`
- **When:** `GET /api/v1alpha1/containers?max_page_size=10` is called
- **Then:** Response has at most 10 containers AND `next_page_token` is non-empty

### TC-U017: ListContainers returns empty array when no containers

- **Requirement:** REQ-API-120
- **Priority:** High
- **Type:** Unit
- **Given:** Mock repository returns an empty list
- **When:** `GET /api/v1alpha1/containers` is called
- **Then:** HTTP status is `200` AND `containers` is an empty JSON array `[]` (not `null` or absent)

### TC-U018: GetContainer returns 200 for existing container

- **Requirement:** REQ-API-130
- **Priority:** High
- **Type:** Unit
- **Given:** Mock repository returns a container for id `"abc-123"`
- **When:** `GET /api/v1alpha1/containers/abc-123` is called
- **Then:** HTTP status is `200` AND body matches the returned container

### TC-U019: GetContainer returns 404 for non-existent container

- **Requirement:** REQ-API-140
- **Priority:** High
- **Type:** Unit
- **Transitively covers:** TC-U025 (Not-found error is distinguishable — mock repository returns a typed not-found error that the handler maps to 404)
- **Given:** Mock repository returns a not-found error for id `"xyz-999"`
- **When:** `GET /api/v1alpha1/containers/xyz-999` is called
- **Then:** HTTP status is `404` AND body is RFC 7807 error with type `NOT_FOUND`

### TC-U020: DeleteContainer returns 204 for existing container

- **Requirement:** REQ-API-150
- **Priority:** High
- **Type:** Unit
- **Given:** Mock repository successfully deletes container `"abc-123"`
- **When:** `DELETE /api/v1alpha1/containers/abc-123` is called
- **Then:** HTTP status is `204` AND body is empty

### TC-U021: DeleteContainer returns 404 for non-existent container

- **Requirement:** REQ-API-160
- **Priority:** High
- **Type:** Unit
- **Transitively covers:** TC-U025 (Not-found error is distinguishable)
- **Given:** Mock repository returns a not-found error for id `"xyz-999"`
- **When:** `DELETE /api/v1alpha1/containers/xyz-999` is called
- **Then:** HTTP status is `404` AND body is RFC 7807 error with type `NOT_FOUND`

### TC-U022: Error responses use RFC 7807 format

- **Requirement:** REQ-API-170
- **Priority:** High
- **Type:** Unit (table-driven across error scenarios)
- **Given:** Any error condition (not found, conflict, validation failure, internal error)
- **When:** The error response is returned
- **Then:** `Content-Type` is `application/problem+json` AND body contains at minimum `type` and `title` fields

### TC-U023: Error types map to correct HTTP status codes

- **Requirement:** REQ-API-180
- **Priority:** High
- **Type:** Unit (table-driven)
- **Given:** Error conditions mapped as follows:

  | Error Condition      | Expected Status | Expected Type    |
  |----------------------|-----------------|------------------|
  | Invalid request body | 400             | INVALID_ARGUMENT |
  | Container not found  | 404             | NOT_FOUND        |
  | Name already exists  | 409             | ALREADY_EXISTS   |
  | Unexpected error     | 500             | INTERNAL         |

- **When:** Each error is mapped to an HTTP response
- **Then:** The status code and error type match the table

### TC-U046: CreateContainer rejects duplicate client-specified id

- **Requirement:** REQ-API-080 (extended to `id` uniqueness — see SC-001)
- **Priority:** High
- **Type:** Unit
- **Given:** Mock repository returns a conflict error when the provided `id` matches an existing container's `dcm.project/dcm-instance-id`
- **When:** `POST /api/v1alpha1/containers?id=existing-id` is called
- **Then:** HTTP status is `409` AND body is RFC 7807 error with type `ALREADY_EXISTS`

### TC-U047: CreateContainer accepts valid boundary IDs

- **Requirement:** REQ-API-050 (positive boundary)
- **Priority:** Medium
- **Type:** Unit (table-driven)
- **Given:** Client-specified IDs at the boundaries of the valid pattern `^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`
- **When:** `POST /api/v1alpha1/containers?id={valid}` is called for each:
  - `"a"` (single character — minimum length)
  - `"ab"` (two characters)
  - `"a"` + 62 chars of `[a-z0-9]` (exactly 63 characters — maximum length)
  - `"a-b"` (dash in middle)
  - `"a0"` (letter followed by digit)
- **Then:** Each returns HTTP `201` (mock repository returns success) AND the response `id` matches the input

### TC-U048: CreateContainer rejects resource constraints where min > max

- **Requirement:** REQ-API-090 (extended — see SC-002)
- **Priority:** High
- **Type:** Unit (table-driven)
- **Given:** Request bodies with invalid resource constraints
- **When:** `POST` is called for each:
  - `resources.cpu.min=4`, `resources.cpu.max=2` (CPU min > max)
  - `resources.memory.min="4GB"`, `resources.memory.max="2GB"` (memory min > max)
- **Then:** Each returns HTTP `400` with RFC 7807 error body containing type `INVALID_ARGUMENT`

### TC-U049: CreateContainer rejects metadata.labels colliding with DCM labels

- **Requirement:** REQ-API-090 (extended — see SC-004)
- **Priority:** High
- **Type:** Unit (table-driven)
- **Given:** Request bodies with `metadata.labels` containing reserved DCM label keys
- **When:** `POST` is called for each:
  - `metadata.labels: {"dcm.project/managed-by": "custom"}`
  - `metadata.labels: {"dcm.project/dcm-instance-id": "custom-id"}`
  - `metadata.labels: {"dcm.project/dcm-service-type": "custom-type"}`
- **Then:** Each returns HTTP `400` with RFC 7807 error body containing type `INVALID_ARGUMENT`

### TC-U078: CreateContainer rejects reserved "health" container ID

- **Requirement:** REQ-HLT-010
- **AC:** AC-HLT-050
- **Priority:** High
- **Type:** Unit
- **Given:** A valid container request body
- **When:** `POST /api/v1alpha1/containers?id=health` is called
- **Then:** HTTP status is `400` AND body is RFC 7807 error with type `INVALID_ARGUMENT` AND detail mentions the ID is reserved

### TC-U079: CreateContainer accepts spec-wrapped body

- **Requirement:** REQ-API-200
- **AC:** AC-API-200
- **Priority:** High
- **Type:** Unit
- **Given:** POST body is `{"spec": {<valid-container-fields>}}`
- **When:** `POST /api/v1alpha1/containers` is called (mock repository returns success)
- **Then:** HTTP status is `201` AND mock repository receives unwrapped Container (not the wrapper)

### TC-U080: Raw Container body rejected by OpenAPI middleware

- **Requirement:** REQ-API-200
- **AC:** AC-API-210
- **Priority:** High
- **Type:** Unit
- **Given:** POST body is a raw Container without `spec` wrapper
- **When:** OpenAPI validation middleware processes the request
- **Then:** HTTP status is `400` with RFC 7807 error containing type `INVALID_ARGUMENT`
- **Referenced by:** TC-U014 (server_validation_test.go)

### TC-U081: CreateContainer accepts provider_hints

- **Requirement:** REQ-API-210
- **AC:** AC-API-220
- **Priority:** High
- **Type:** Unit
- **Given:** POST body includes `"provider_hints": {"gpu": true}` in the container spec
- **When:** `POST /api/v1alpha1/containers` is called (mock repository returns success)
- **Then:** HTTP status is `201` AND provider_hints do not affect store call

### TC-U082: ExternalServiceType is required at startup

- **Requirement:** REQ-XC-CFG-030
- **AC:** AC-XC-CFG-030
- **Priority:** High
- **Type:** Unit
- **Given:** SP_K8S_EXTERNAL_SVC_TYPE is not set (absent)
- **When:** Config is loaded
- **Then:** An error is returned identifying SP_K8S_EXTERNAL_SVC_TYPE as required

### TC-U083: ExternalServiceType rejects invalid values

- **Requirement:** REQ-XC-CFG-030
- **AC:** AC-XC-CFG-030
- **Priority:** High
- **Type:** Unit
- **Given:** SP_K8S_EXTERNAL_SVC_TYPE is set to "ClusterIP" (or other invalid value)
- **When:** Config is loaded
- **Then:** An error is returned stating the value must be LoadBalancer or NodePort

### TC-U084: ExternalServiceType accepts LoadBalancer

- **Requirement:** REQ-XC-CFG-030
- **AC:** AC-XC-CFG-030
- **Priority:** High
- **Type:** Unit
- **Given:** SP_K8S_EXTERNAL_SVC_TYPE is set to "LoadBalancer"
- **When:** Config is loaded
- **Then:** Config loads successfully with ExternalServiceType="LoadBalancer"

### TC-U085: ExternalServiceType accepts NodePort

- **Requirement:** REQ-XC-CFG-030
- **AC:** AC-XC-CFG-030
- **Priority:** High
- **Type:** Unit
- **Given:** SP_K8S_EXTERNAL_SVC_TYPE is set to "NodePort"
- **When:** Config is loaded
- **Then:** Config loads successfully with ExternalServiceType="NodePort"

### TC-U086: ExternalServiceType rejects empty string

- **Requirement:** REQ-XC-CFG-030
- **AC:** AC-XC-CFG-030
- **Priority:** High
- **Type:** Unit
- **Given:** SP_K8S_EXTERNAL_SVC_TYPE is set to "" (empty string)
- **When:** Config is loaded
- **Then:** An error is returned stating the value must be LoadBalancer or NodePort

### TC-U050: ListContainers rejects invalid page_token

- **Requirement:** REQ-API-100 (see SC-006)
- **Priority:** High
- **Type:** Unit
- **Given:** Mock repository returns an invalid-argument error for an undecodable `page_token`
- **When:** `GET /api/v1alpha1/containers?page_token=not-a-valid-token` is called
- **Then:** HTTP status is `400` AND body is RFC 7807 error with type `INVALID_ARGUMENT`

### TC-U051: Handler returns 500 INTERNAL for unexpected store errors

- **Requirement:** REQ-API-180
- **Priority:** High
- **Type:** Unit
- **Given:** Mock repository returns a generic (non-typed) error from any operation
- **When:** The handler processes the response
- **Then:** HTTP status is `500` AND body is RFC 7807 error with type `INTERNAL`

### TC-U070: ResponseErrorHandlerFunc returns RFC 7807

- **Requirement:** REQ-HTTP-091
- **Priority:** High
- **Type:** Unit
- **Given:** The strict handler adapter's `ResponseErrorHandlerFunc` is configured
- **When:** It is invoked with an error
- **Then:** The response MUST be HTTP 500 with `Content-Type: application/problem+json`
- **And** the body MUST be RFC 7807 with type `INTERNAL`
- **And** the body MUST NOT contain the raw error message

### TC-U071: Handler error responses include instance field

- **Requirement:** REQ-XC-ERR-030
- **Priority:** High
- **Type:** Unit
- **Given:** A handler error occurs (mapCreateError, mapGetError, mapDeleteError, mapListError)
- **When:** The error response is returned
- **Then:** The `instance` field MUST be set to the request path

### TC-U073: scrubValidationError handles RequiredParamError

- **Requirement:** REQ-HTTP-090
- **Priority:** Medium
- **Type:** Unit
- **Given:** A `RequiredParamError` with `ParamName="page_token"`
- **When:** `scrubValidationError` is called
- **Then:** The result is `missing required parameter "page_token"`

### TC-U074: scrubValidationError handles RequiredHeaderError

- **Requirement:** REQ-HTTP-090
- **Priority:** Medium
- **Type:** Unit
- **Given:** A `RequiredHeaderError` with `ParamName="X-Request-ID"`
- **When:** `scrubValidationError` is called
- **Then:** The result is `missing required header "X-Request-ID"`

### TC-U075: scrubValidationError handles UnescapedCookieParamError

- **Requirement:** REQ-HTTP-090
- **Priority:** Medium
- **Type:** Unit
- **Given:** An `UnescapedCookieParamError` with `ParamName="session_id"`
- **When:** `scrubValidationError` is called
- **Then:** The result is `invalid cookie parameter "session_id"`

### TC-U076: scrubValidationError handles UnmarshalingParamError

- **Requirement:** REQ-HTTP-090
- **Priority:** Medium
- **Type:** Unit
- **Given:** An `UnmarshalingParamError` with `ParamName="filter"`
- **When:** `scrubValidationError` is called
- **Then:** The result is `invalid value for parameter "filter"` AND does not contain raw unmarshal error

### TC-U077: scrubValidationError handles TooManyValuesForParamError

- **Requirement:** REQ-HTTP-090
- **Priority:** Medium
- **Type:** Unit
- **Given:** A `TooManyValuesForParamError` with `ParamName="sort"` and `Count=3`
- **When:** `scrubValidationError` is called
- **Then:** The result is `too many values for parameter "sort"`

---

## 4 · Status Reconciliation Logic

> **Suggested Ginkgo structure:** `Describe("Status Reconciliation")`

### TC-U031: Pod status takes precedence over Deployment status

- **Requirement:** REQ-MON-050
- **Priority:** High
- **Type:** Unit
- **Transitively covers:** TC-U029 (Pod phase maps to DCM status — Running case)
- **Given:** Both a Deployment (with Available=True) and a Pod (phase=Running) exist for instance `"abc-123"`
- **When:** Status reconciliation is performed
- **Then:** The resulting DCM status is `RUNNING` (derived from Pod phase, not Deployment conditions)

### TC-U032: Deployment fallback — PENDING when Available=False

- **Requirement:** REQ-MON-060, REQ-MON-080
- **Priority:** High
- **Type:** Unit
- **Given:** A Deployment exists with condition `Available=False` AND no Pod exists
- **When:** Status reconciliation is performed
- **Then:** The resulting DCM status is `PENDING`

### TC-U033: Deployment fallback — FAILED when ReplicaFailure=True

- **Requirement:** REQ-MON-060, REQ-MON-080
- **Priority:** High
- **Type:** Unit
- **Given:** A Deployment exists with condition `ReplicaFailure=True` AND no Pod exists
- **When:** Status reconciliation is performed
- **Then:** The resulting DCM status is `FAILED`

### TC-U034: Deployment fallback — FAILED when Replicas=0

- **Requirement:** REQ-MON-060, REQ-MON-080
- **Priority:** High
- **Type:** Unit
- **Given:** A Deployment exists with `Replicas=0` AND no Pod exists
- **When:** Status reconciliation is performed
- **Then:** The resulting DCM status is `FAILED`

### TC-U060: Deployment fallback — PENDING when Available=True but no Pod exists

- **Requirement:** REQ-MON-060 (see SC-007)
- **Priority:** High
- **Type:** Unit
- **Given:** A Deployment exists with condition `Available=True` AND no Pod exists
- **When:** Status reconciliation is performed
- **Then:** The resulting DCM status is `PENDING` (no Pod means PENDING regardless of Deployment conditions, unless ReplicaFailure=True or Replicas=0)

### TC-U035: DELETED status when neither resource exists

- **Requirement:** REQ-MON-070, REQ-MON-080
- **Priority:** High
- **Type:** Unit
- **Given:** Neither Deployment nor Pod exists for a previously tracked instance
- **When:** Status reconciliation is performed for a deletion event
- **Then:** The resulting DCM status is `DELETED`

---

## Utility Test Case Index

Utility and helper functions are tested **transitively** through the
behavioural tests listed above and in the integration test plan. Each utility
TC-ID is preserved for requirements traceability but does **not** map to a
dedicated test class or `Describe` block.

### Structural Contracts

#### TC-U007: GetHealth only uses health check, not CRUD methods

- **Requirement:** REQ-HLT-040
- **Priority:** Medium
- **Type:** Unit (structural)
- **Given:** The `Handler` struct (which includes a `store` field for container CRUD and health checking)
- **When:** `GetHealth` is inspected
- **Then:** It uses only `startTime`, `version`, and `store.CheckHealth` — it never accesses CRUD methods (Create, Get, List, Delete) or any other external dependency.
- **Referenced by:** TC-U005 (handler constructed with mock repo; GetHealth succeeds without CRUD methods configured)

#### TC-U008: Handler implements StrictServerInterface

- **Requirement:** REQ-API-010
- **Priority:** High
- **Type:** Unit (compile-time assertion)
- **Given:** The handler type exists
- **When:** A compile-time type assertion to `StrictServerInterface` is performed
- **Then:** The assertion compiles and succeeds
- **Referenced by:** TC-U009 (first handler test — `var _ StrictServerInterface = (*Handler)(nil)` in test file)

#### TC-U072: StatusPublisher interface satisfied by NATS publisher

- **Requirement:** REQ-MON-170
- **Priority:** High
- **Type:** Unit (compile-time assertion)
- **Given:** The StatusPublisher interface is defined
- **When:** A compile-time type assertion of the NATS publisher to StatusPublisher is performed
- **Then:** The assertion compiles and succeeds
- **Referenced by:** `internal/monitoring/helpers_test.go` — `var _ StatusPublisher = (*NATSPublisher)(nil)`

#### TC-U079: Debouncer Stop waits for in-flight publish callbacks

- **Requirement:** REQ-MON-131
- **AC:** AC-MON-130
- **Priority:** High
- **Type:** Unit
- **Given:** A Debouncer with a blocking publishFn that has started executing
- **When:** Stop() is called concurrently
- **Then:** Stop() blocks until the in-flight publishFn completes, and no further publishes occur after Stop() returns

#### TC-U024: ContainerRepository interface is satisfied by implementation

- **Requirement:** REQ-STR-010
- **Priority:** High
- **Type:** Unit (compile-time assertion)
- **Given:** The `ContainerRepository` interface is defined with `Create`, `Get`, `List`, `Delete`
- **When:** A compile-time type assertion of the K8s store to `ContainerRepository` is performed
- **Then:** The assertion compiles and succeeds
- **Referenced by:** TC-I009 (first K8s store integration test — `var _ ContainerRepository = (*K8sStore)(nil)` in test file)

### Error Types

#### TC-U025: Not-found error is distinguishable

- **Requirement:** REQ-STR-080
- **Priority:** High
- **Type:** Unit
- **Given:** A not-found error is created by the store error package
- **When:** The error is inspected with `errors.Is` or `errors.As`
- **Then:** It is correctly identified as a not-found error AND distinguishable from conflict and other errors
- **Referenced by:** TC-U019 (GetContainer 404), TC-U021 (DeleteContainer 404), TC-I032 (Get not-found integration), TC-I039 (Delete not-found integration)

#### TC-U026: Conflict error is distinguishable

- **Requirement:** REQ-STR-080
- **Priority:** High
- **Type:** Unit
- **Given:** A conflict error is created by the store error package
- **When:** The error is inspected with `errors.Is` or `errors.As`
- **Then:** It is correctly identified as a conflict error AND distinguishable from not-found and other errors
- **Referenced by:** TC-U013 (CreateContainer 409), TC-I028 (Create conflict integration)

### Resource Mapping & Conversion

#### TC-U027: CPU values map to Kubernetes resource quantities

- **Requirement:** REQ-K8S-040
- **Priority:** High
- **Type:** Unit
- **Given:** Container CPU with `min=1`, `max=2`
- **When:** CPU is converted to Kubernetes resource quantities
- **Then:** `requests.cpu` is `"1"` AND `limits.cpu` is `"2"`
- **Referenced by:** TC-I012 (CPU resources integration test)

#### TC-U028: Memory units convert from schema format to Kubernetes format

- **Requirement:** REQ-K8S-050
- **Priority:** High
- **Type:** Unit (table-driven)
- **Given:** Memory values in schema format
- **When:** Each is converted to Kubernetes format:

  | Input     | Expected Output |
  |-----------|-----------------|
  | `"1MB"`   | `"1Mi"`         |
  | `"512MB"` | `"512Mi"`       |
  | `"1GB"`   | `"1Gi"`         |
  | `"2GB"`   | `"2Gi"`         |
  | `"1TB"`   | `"1Ti"`         |
  | `"10TB"`  | `"10Ti"`        |

- **Then:** Each output matches the expected Kubernetes format
- **Referenced by:** TC-I013 (memory resources integration test)

#### TC-U029: Pod phase maps to DCM container status

- **Requirement:** REQ-K8S-230, REQ-MON-080
- **Priority:** High
- **Type:** Unit (table-driven)
- **Given:** Pods with various phases
- **When:** Status mapping is applied:

  | Pod Phase  | Expected DCM Status |
  |------------|---------------------|
  | Pending    | PENDING             |
  | Running    | RUNNING             |
  | Failed     | FAILED              |
  | Unknown    | UNKNOWN             |

- **Then:** Each produces the correct DCM ContainerStatus
- **Referenced by:** TC-U031 (Running case via reconciliation), TC-I062 (Pending), TC-I063 (Failed), TC-I064 (Unknown)

#### TC-U030: Pod Succeeded phase is explicitly ignored

- **Requirement:** REQ-K8S-230, REQ-MON-080
- **Priority:** High
- **Type:** Unit
- **Given:** A Pod with phase `Succeeded`
- **When:** Status mapping is applied
- **Then:** The result indicates this phase should be ignored (no CloudEvent published)
- **Referenced by:** TC-I065 (Succeeded phase produces no event)

### CloudEvent Construction

#### TC-U036: CloudEvent has correct v1.0 structure

- **Requirement:** REQ-MON-090, REQ-MON-095
- **Priority:** High
- **Type:** Unit
- **Given:** A status change for instance `"abc-123"` with provider name `"k8s-sp"` and status `RUNNING`
- **When:** A CloudEvent is constructed
- **Then:**
  - `specversion` is `"1.0"`
  - `id` is a non-empty unique identifier
  - `source` is `"dcm/providers/k8s-sp"`
  - `type` is `"dcm.status.container"`
  - `subject` is `"dcm.container"`
  - `datacontenttype` is `"application/json"`
  - `data` contains `{"id": "abc-123", "status": "RUNNING", "message": "..."}`
- **Referenced by:** TC-I047 (NATS publishing verifies CloudEvent structure)

#### TC-U037: FAILED CloudEvent includes failure reason in message

- **Requirement:** REQ-MON-150, REQ-MON-095
- **Priority:** High
- **Type:** Unit
- **Given:** A Pod with failed container status reason `"CrashLoopBackOff"` for instance `"abc-123"`
- **When:** A FAILED status CloudEvent is constructed
- **Then:** The `data.id` field is `"abc-123"` AND `data.status` is `"FAILED"` AND `data.message` includes `"CrashLoopBackOff"`
- **Referenced by:** TC-I048 (FAILED NATS event includes failure reason)

### Debounce Logic

#### TC-U038: Only last event within debounce window is published

- **Requirement:** REQ-MON-110
- **Priority:** High
- **Type:** Unit
- **Given:** A debouncer with interval of 500ms
- **When:** Three status changes occur within 500ms: `PENDING` → `RUNNING` → `FAILED`
- **Then:** Only one event is published AND its status is `FAILED`
- **Referenced by:** TC-I066 (debounce windowed suppression integration test)

#### TC-U039: Events after debounce window are published separately

- **Requirement:** REQ-MON-110
- **Priority:** Medium
- **Type:** Unit
- **Given:** A debouncer with interval of 500ms
- **When:** A status change occurs, the debounce window elapses fully, then another status change occurs
- **Then:** Two separate events are published
- **Referenced by:** TC-I067 (post-window events integration test)

### Instance ID & Indexer

#### TC-U040: Instance ID extracted from dcm.project/dcm-instance-id label

- **Requirement:** REQ-MON-120
- **Priority:** High
- **Type:** Unit
- **Given:** A Kubernetes resource with label `dcm.project/dcm-instance-id="abc-123"`
- **When:** Instance ID extraction is performed
- **Then:** The result is `"abc-123"`
- **Referenced by:** TC-I043 (informer indexer integration), TC-I044 (Pod update triggers reconciliation)

#### TC-U041: Missing dcm.project/dcm-instance-id label handled gracefully

- **Requirement:** REQ-MON-120
- **Priority:** Medium
- **Type:** Unit
- **Given:** A Kubernetes resource without the `dcm.project/dcm-instance-id` label
- **When:** Instance ID extraction is attempted
- **Then:** An empty string or error is returned (resource is skipped, not panicked)
- **Referenced by:** TC-I042 (informers filter non-DCM resources)

#### TC-U042: Indexer function returns dcm.project/dcm-instance-id value

- **Requirement:** REQ-MON-040
- **Priority:** High
- **Type:** Unit
- **Given:** A Kubernetes object with label `dcm.project/dcm-instance-id="abc-123"`
- **When:** The custom indexer function is called
- **Then:** It returns `["abc-123"]`
- **Referenced by:** TC-I043 (informer indexer enables lookup by dcm.project/dcm-instance-id)

### OpenAPI Contract Enforcement

#### TC-U052: Invalid serviceType value rejected

- **Requirement:** REQ-API-090
- **Priority:** High
- **Type:** Unit (validation sub-case)
- **Given:** A request body with `serviceType="not-a-service-type"`
- **When:** OpenAPI validation is applied
- **Then:** The request is rejected with 400 INVALID_ARGUMENT
- **Referenced by:** TC-U014 (CreateContainer validates request body)

#### TC-U053: Invalid metadata.name format rejected

- **Requirement:** REQ-API-090
- **Priority:** High
- **Type:** Unit (validation sub-case)
- **Given:** A request body with `metadata.name="Invalid_Name!"`
- **When:** OpenAPI validation is applied
- **Then:** The request is rejected with 400 INVALID_ARGUMENT
- **Referenced by:** TC-U014 (CreateContainer validates request body)

#### TC-U054: Invalid memory format rejected

- **Requirement:** REQ-API-090
- **Priority:** High
- **Type:** Unit (validation sub-case)
- **Given:** A request body with `resources.memory.min="10XB"`
- **When:** OpenAPI validation is applied
- **Then:** The request is rejected with 400 INVALID_ARGUMENT
- **Referenced by:** TC-U014 (CreateContainer validates request body)

#### TC-U055: CPU value below minimum rejected

- **Requirement:** REQ-API-090
- **Priority:** High
- **Type:** Unit (validation sub-case)
- **Given:** A request body with `resources.cpu.min=0`
- **When:** OpenAPI validation is applied
- **Then:** The request is rejected with 400 INVALID_ARGUMENT
- **Referenced by:** TC-U014 (CreateContainer validates request body)

#### TC-U056: Port out of range rejected

- **Requirement:** REQ-API-090
- **Priority:** High
- **Type:** Unit (validation sub-case)
- **Given:** A request body with `network.ports[0].containerPort=99999`
- **When:** OpenAPI validation is applied
- **Then:** The request is rejected with 400 INVALID_ARGUMENT
- **Referenced by:** TC-U014 (CreateContainer validates request body)

#### TC-U057: max_page_size boundary and containerId path parameter enforcement

- **Requirement:** REQ-HTTP-090
- **Priority:** High
- **Type:** Unit (validation sub-case)
- **Given:** Requests with out-of-range `max_page_size` values (0, -1, 1001) or invalid `containerId` path parameter format
- **When:** OpenAPI validation is applied
- **Then:** Each is rejected with 400 Bad Request
- **Referenced by:** TC-I008 (Malformed requests integration test)

#### TC-U058: Invalid containerId path parameter rejected

- **Requirement:** REQ-HTTP-090
- **Priority:** High
- **Type:** Unit (validation sub-case)
- **Given:** A request with an invalid `containerId` path parameter (e.g., empty string or characters outside the allowed pattern)
- **When:** OpenAPI validation is applied
- **Then:** The request is rejected with 400 Bad Request
- **Referenced by:** TC-I008 (Malformed requests integration test)

#### TC-U059: network object without ports field accepted

- **Requirement:** REQ-K8S-155
- **AC:** AC-K8S-152
- **Priority:** Medium
- **Type:** Unit (validation sub-case)
- **Given:** A request body with `network: {}` (object present, but no `ports` field)
- **When:** The request is processed
- **Then:** The request is accepted (201) — no Service is created, no error
- **Referenced by:** TC-I111 (Network without ports creates no Service)

### Registration Payload

#### TC-U061: Registrar construction fails for an invalid registration URL

- **Requirement:** REQ-REG-070
- **Priority:** High
- **Type:** Unit
- **Given:** A configuration containing an invalid registration URL (e.g., `"://invalid-url"`)
- **When:** The registrar is constructed
- **Then:** Construction fails with an error indicating the URL is invalid
- **Referenced by:** TC-I059 (registration uses DCM client library)

#### TC-U062: Registration payload is independent of subsequent configuration changes

- **Requirement:** REQ-REG-020
- **Priority:** High
- **Type:** Unit
- **Given:** A configuration with known values for display name, region, and zone
- **When:** The registration payload is constructed and the source configuration is subsequently modified
- **Then:** The payload retains the original values, unaffected by later configuration changes
- **Referenced by:** TC-U043 (payload contains all required fields)

#### TC-U063: Configuration loading fails when required fields are absent

- **Requirement:** REQ-XC-CFG-010
- **Priority:** High
- **Type:** Unit
- **Given:** The required configuration values (provider name, provider endpoint, DCM registration URL, and external service type) are not provided
- **When:** The configuration is loaded
- **Then:** An error is returned identifying each missing required field
- **Referenced by:** TC-U002 (configuration loading)

#### TC-U043: Payload contains all configured fields

- **Requirement:** REQ-REG-020
- **Priority:** High
- **Type:** Unit
- **Given:** Provider config: `name="k8s-sp"`, `display_name="K8s Container SP"`, `endpoint="https://sp.example.com"`
- **When:** Registration payload is constructed
- **Then:**
  - `name` is `"k8s-sp"`
  - `service_type` is `"container"`
  - `schema_version` is `"v1alpha1"`
  - `display_name` is `"K8s Container SP"`
  - `endpoint` is `"https://sp.example.com/api/v1alpha1/containers"`
  - `operations` contains `["CREATE", "DELETE", "READ"]`
- **Referenced by:** TC-I054 (registration payload integration test)

#### TC-U044: Payload includes region and zone metadata when configured

- **Requirement:** REQ-REG-020
- **Priority:** Medium
- **Type:** Unit
- **Given:** Provider config with `region="us-east-1"` and `zone="us-east-1a"`
- **When:** Registration payload is constructed
- **Then:** `metadata.region_code` is `"us-east-1"` AND `metadata.zone` is `"us-east-1a"`
- **Referenced by:** TC-I054 (registration payload integration test)

#### TC-U045: Payload omits metadata when region and zone not configured

- **Requirement:** REQ-REG-020
- **Priority:** Medium
- **Type:** Unit
- **Given:** Provider config without region or zone
- **When:** Registration payload is constructed
- **Then:** `metadata.region_code` and `metadata.zone` are absent from the payload
- **Referenced by:** TC-I068 (registration without metadata integration test)

#### TC-U064: Payload omits display_name when not configured

- **Requirement:** REQ-REG-020
- **Priority:** Medium
- **Type:** Unit
- **Given:** Provider config: `name="k8s-sp"`, `endpoint="https://sp.example.com"` with NO `display_name` configured
- **When:** Registration payload is constructed
- **Then:** `display_name` is absent from the payload (nil pointer)
- **Referenced by:** TC-I068 (registration without optional fields integration test)

#### TC-U066: ~~Composite handler delegates GetHealth to health sub-handler~~ (RETIRED)

- **Status:** RETIRED
- **Reason:** Composite handler delegation layer removed; health behavior covered directly by TC-U005 in the container handler package.

#### TC-U067: Valid request passes OpenAPI middleware

- **Requirement:** REQ-API-090, REQ-HTTP-090
- **Priority:** High
- **Type:** Unit (validation sub-case)
- **Given:** A valid container request body with all required fields
- **When:** POST `/api/v1alpha1/containers` is sent through the OpenAPI validation middleware
- **Then:** The response is 201 with a valid container body (service_type, metadata.name present)
- **Referenced by:** TC-U014 (CreateContainer validates request body)

#### TC-U069: CreateContainer accepts and propagates non-reserved user labels

- **Requirement:** REQ-API-090 (extended — see SC-004)
- **Priority:** High
- **Type:** Unit
- **Given:** A valid request body with `metadata.labels: {"team": "platform", "env": "dev"}` (non-reserved keys)
- **When:** `POST /api/v1alpha1/containers` is called (mock repository returns success)
- **Then:** HTTP status is `201` AND the labels passed to the store match the input AND the response container's labels match the input
- **Referenced by:** TC-U049 (positive complement to reserved-label rejection)

#### TC-U068: containerIDPattern matches OpenAPI spec pattern

- **Requirement:** REQ-API-050
- **Priority:** High
- **Type:** Unit (contract test)
- **Given:** The embedded OpenAPI spec returned by `v1alpha1.GetSwagger()`
- **When:** The `id` query parameter pattern from `POST /api/v1alpha1/containers` is extracted
- **Then:** It matches `containerIDPattern.String()` exactly
- **Referenced by:** TC-U012 (CreateContainer rejects invalid IDs), TC-U047 (valid boundary IDs)

#### TC-U065: ~~Unimplemented endpoints return 501~~ (RETIRED)

- **Status:** RETIRED
- **Reason:** Tests generated code behavior (`oapigen.Unimplemented`), not project logic; composite handler removed. REQ-API-010 remains covered by TC-U008 (via TC-U009).

---

## Coverage Matrix

| Requirement   | Test Cases                        | Status  |
|---------------|-----------------------------------|---------|
| REQ-HTTP-050  | TC-U002, TC-U004                  | Covered |
| REQ-HTTP-070  | TC-I102, TC-I104, TC-I105, TC-I106 (integration)  | Covered |
| REQ-HTTP-090  | TC-U057 (via TC-I008), TC-U058 (via TC-I008), TC-U067 (via TC-U014), TC-U073–TC-U077 | Covered |
| REQ-HTTP-091  | TC-U070                           | Covered |
| REQ-HLT-010   | TC-U005, TC-U078                  | Covered |
| REQ-HLT-020   | TC-U005, TC-U006, TC-U087, TC-U088 | Covered |
| REQ-HLT-030   | TC-U005 (transitively via generated `VisitGetHealthResponse` + TC-I001/I002) | Covered |
| REQ-HLT-040   | TC-U007 (via TC-U005)             | Covered |
| REQ-HLT-050   | TC-U005, TC-U089 (via TC-I116)    | Covered |
| REQ-HLT-060   | TC-U087, TC-U088                  | Covered |
| REQ-HLT-070   | TC-U089                           | Covered |
| REQ-API-010   | TC-U008 (via TC-U009)             | Covered |
| REQ-API-020   | TC-U009                           | Covered |
| REQ-API-030   | TC-U010                           | Covered |
| REQ-API-040   | TC-U011                           | Covered |
| REQ-API-050   | TC-U012, TC-U047, TC-U068         | Covered |
| REQ-API-060   | TC-U009                           | Covered |
| REQ-API-070   | TC-U009                           | Covered |
| REQ-API-080   | TC-U013, TC-U046                  | Covered |
| REQ-API-090   | TC-U014, TC-U048, TC-U049, TC-U052–TC-U056, TC-U067 (via TC-U014), TC-U069 | Covered |
| REQ-API-100   | TC-U015, TC-U050                  | Covered |
| REQ-API-110   | TC-U016                           | Covered |
| REQ-API-120   | TC-U017                           | Covered |
| REQ-API-130   | TC-U018                           | Covered |
| REQ-API-140   | TC-U019                           | Covered |
| REQ-API-150   | TC-U020                           | Covered |
| REQ-API-160   | TC-U021                           | Covered |
| REQ-API-170   | TC-U022                           | Covered |
| REQ-API-180   | TC-U023, TC-U051                  | Covered |
| REQ-API-200   | TC-U079, TC-U080                  | Covered |
| REQ-API-210   | TC-U081                           | Covered |
| REQ-STR-010   | TC-U024 (via TC-I009)             | Covered |
| REQ-STR-080   | TC-U025 (via TC-U019/U021), TC-U026 (via TC-U013) | Covered |
| REQ-K8S-040   | TC-U027 (via TC-I012)             | Covered |
| REQ-K8S-155   | TC-U059 (via TC-I111)             | Covered |
| REQ-K8S-050   | TC-U028 (via TC-I013)             | Covered |
| REQ-K8S-230   | TC-U029 (via TC-U031, TC-I062–I064), TC-U030 (via TC-I065) | Covered |
| REQ-MON-040   | TC-U042 (via TC-I043)             | Covered |
| REQ-MON-050   | TC-U031                           | Covered |
| REQ-MON-060   | TC-U032, TC-U033, TC-U034, TC-U060 | Covered |
| REQ-MON-070   | TC-U035                           | Covered |
| REQ-MON-080   | TC-U029–TC-U035                   | Covered |
| REQ-MON-090   | TC-U036 (via TC-I047)             | Covered |
| REQ-MON-095   | TC-U036 (via TC-I047), TC-U037 (via TC-I048) | Covered |
| REQ-MON-110   | TC-U038 (via TC-I066), TC-U039 (via TC-I067) | Covered |
| REQ-MON-120   | TC-U040 (via TC-I043/I044), TC-U041 (via TC-I042) | Covered |
| REQ-MON-150   | TC-U037 (via TC-I048)             | Covered |
| REQ-REG-020   | TC-U043 (via TC-I054), TC-U044 (via TC-I054), TC-U045 (via TC-I068), TC-U062, TC-U064 (via TC-I068) | Covered |
| REQ-XC-ID-010 | TC-U009 (id in path), TC-U024 (via TC-I009, metadata.name as generateName prefix) | Covered |
| REQ-XC-ID-020 | TC-U013, TC-U046                  | Covered |
| REQ-XC-ERR-010| TC-U022                           | Covered |
| REQ-XC-ERR-020| TC-U022                           | Covered |
| REQ-XC-ERR-030| TC-U071                           | Covered |
| REQ-XC-ERR-040| TC-U051, TC-I104 (integration)    | Covered |
| REQ-REG-070   | TC-U061                           | Covered |
| REQ-XC-CFG-010| TC-U002, TC-U004, TC-U063         | Covered |
| REQ-XC-CFG-020| TC-U063                           | Covered |
| REQ-XC-CFG-030| TC-U082, TC-U083, TC-U084, TC-U085 | Covered |
| REQ-MON-131   | TC-U079                           | Covered |
| REQ-MON-170   | TC-U072, TC-U079                  | Covered |

**Total:** 76 test case IDs (2 retired: TC-U065, TC-U066) — 45 in behavioural
test classes, 31 in the utility index (tested transitively through higher-level
behavioural and integration tests).

> Requirements not listed above (REQ-HTTP-010–040, REQ-HTTP-080,
> REQ-STR-020–070, REQ-K8S-010–270 excluding 040/050/230, REQ-MON-010–030,
> REQ-MON-100, REQ-MON-130–145, REQ-MON-160, REQ-MON-180–190,
> REQ-REG-010, REQ-REG-030–070,
> REQ-XC-LBL-010, REQ-XC-LOG-010–020) are covered in the integration test
> plan.

---

## Notes

- **Table-driven tests:** TC-U012, TC-U014, TC-U023, TC-U047, TC-U048, TC-U049 should be implemented as Ginkgo `DescribeTable` / `Entry` for conciseness.
- **Mock repository:** Handler tests (TC-U009–TC-U023) require a mock implementation of `ContainerRepository`. Use Gomega's mock support or `testify/mock` wrapped for Ginkgo.
- **Compile-time checks:** TC-U008 and TC-U024 are implemented as `var _ StrictServerInterface = (*Handler)(nil)` in their respective test files. They do not need their own `It` block.
- **Time-sensitive tests:** TC-U006 depends on time. Use a clock interface or inject a time function to avoid flaky tests.
- **REQ-HLT-040 (lightweight):** TC-U007 verifies the handler's structural simplicity. Runtime performance is a design constraint validated during code review, not a functional test.
- **Utility transitive coverage:** Utility TCs (TC-U007/U008/U024–U030/U036–U045/U052–U059/U067–U068) have no dedicated `Describe` blocks. Their coverage is achieved through the behavioural tests that reference them. The integration test plan documents the corresponding integration-level transitive references.
