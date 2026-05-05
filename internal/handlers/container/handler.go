// Package container implements the container API request handlers.
package container

import (
	"context"
	"log/slog"
	"time"

	oapigen "github.com/dcm-project/k8s-container-service-provider/internal/api/server"
	"github.com/dcm-project/k8s-container-service-provider/internal/store"
	"github.com/dcm-project/k8s-container-service-provider/internal/util"
	"github.com/google/uuid"
)

// Handler implements oapigen.StrictServerInterface for container CRUD
// operations and the health endpoint. It delegates persistence to a
// store.ContainerRepository and maps store errors to typed OpenAPI responses.
type Handler struct {
	store     store.ContainerRepository
	logger    *slog.Logger
	startTime time.Time
	version   string
}

// NewHandler creates a Handler backed by the given repository.
func NewHandler(repo store.ContainerRepository, logger *slog.Logger, startTime time.Time, version string) *Handler {
	return &Handler{
		store:     repo,
		logger:    logger,
		startTime: startTime,
		version:   version,
	}
}

const containersBasePath = "/api/v1alpha1/containers"

func (h *Handler) CreateContainer(ctx context.Context, req oapigen.CreateContainerRequestObject) (oapigen.CreateContainerResponseObject, error) {
	spec := req.Body.Spec

	var id string
	if req.Params.Id != nil {
		id = *req.Params.Id
	} else {
		id = uuid.New().String()
	}

	requestPath := containersBasePath

	if err := validateContainerID(id); err != nil {
		return newCreateError400(err.Error(), requestPath), nil
	}

	if err := validateResources(spec.Resources); err != nil {
		return newCreateError400(err.Error(), requestPath), nil
	}

	if err := validateUserLabels(spec.Metadata.Labels); err != nil {
		return newCreateError400(err.Error(), requestPath), nil
	}

	result, err := h.store.Create(ctx, spec, id)
	if err != nil {
		return h.mapCreateError(err, requestPath), nil
	}
	return oapigen.CreateContainer201JSONResponse(*result), nil
}

func (h *Handler) GetContainer(ctx context.Context, req oapigen.GetContainerRequestObject) (oapigen.GetContainerResponseObject, error) {
	requestPath := containersBasePath + "/" + req.ContainerId
	result, err := h.store.Get(ctx, req.ContainerId)
	if err != nil {
		return h.mapGetError(err, requestPath), nil
	}
	return oapigen.GetContainer200JSONResponse(*result), nil
}

func (h *Handler) DeleteContainer(ctx context.Context, req oapigen.DeleteContainerRequestObject) (oapigen.DeleteContainerResponseObject, error) {
	requestPath := containersBasePath + "/" + req.ContainerId
	if err := h.store.Delete(ctx, req.ContainerId); err != nil {
		return h.mapDeleteError(err, requestPath), nil
	}
	return oapigen.DeleteContainer204Response{}, nil
}

func (h *Handler) ListContainers(ctx context.Context, req oapigen.ListContainersRequestObject) (oapigen.ListContainersResponseObject, error) {
	// maxPageSize validation chain:
	//   - OpenAPI middleware enforces max_page_size ∈ [1,1000] (invalid → 400).
	//   - When omitted (nil), 0 is passed to the store which defaults to 50.
	// No handler-layer clamping is needed.
	var maxPageSize int32
	if req.Params.MaxPageSize != nil {
		maxPageSize = *req.Params.MaxPageSize
	}

	var pageToken string
	if req.Params.PageToken != nil {
		pageToken = *req.Params.PageToken
	}

	result, err := h.store.List(ctx, maxPageSize, pageToken)
	if err != nil {
		return h.mapListError(err, containersBasePath), nil
	}
	return oapigen.ListContainers200JSONResponse(*result), nil
}

// GetHealth returns the service health status including uptime and version.
// It checks the backing K8s cluster liveness and returns "unhealthy" if
// the cluster is unreachable.
func (h *Handler) GetHealth(ctx context.Context, _ oapigen.GetHealthRequestObject) (oapigen.GetHealthResponseObject, error) {
	status := "healthy"
	if h.store != nil {
		if err := h.store.CheckHealth(ctx); err != nil {
			status = "unhealthy"
		}
	}

	uptime := max(0, int(time.Since(h.startTime).Seconds()))
	return oapigen.GetHealth200JSONResponse{
		Status:  status,
		Type:    util.Ptr("k8s-container-service-provider.dcm.io/health"),
		Path:    util.Ptr("health"),
		Uptime:  &uptime,
		Version: util.Ptr(h.version),
	}, nil
}
