package container_test

import (
	"context"
	"time"

	v1alpha1 "github.com/dcm-project/k8s-container-service-provider/api/v1alpha1"
	oapigen "github.com/dcm-project/k8s-container-service-provider/internal/api/server"
	"github.com/dcm-project/k8s-container-service-provider/internal/handlers/container"
	"github.com/dcm-project/k8s-container-service-provider/internal/store"
)

const testNamespace = "test-ns"

// ---------------------------------------------------------------------------
// Compile-time assertions
// ---------------------------------------------------------------------------

// TC-U009 (via TC-U008): Handler implements StrictServerInterface.
var _ oapigen.StrictServerInterface = (*container.Handler)(nil)

// mockContainerRepository implements store.ContainerRepository.
var _ store.ContainerRepository = (*mockContainerRepository)(nil)

// ---------------------------------------------------------------------------
// Mock repository
// ---------------------------------------------------------------------------

// mockContainerRepository is a test double for store.ContainerRepository.
// Each method delegates to a configurable function field. Unconfigured methods
// panic so that unexpected calls are immediately visible.
type mockContainerRepository struct {
	CreateFunc      func(ctx context.Context, spec v1alpha1.ContainerSpec, id string) (*v1alpha1.Container, error)
	GetFunc         func(ctx context.Context, containerID string) (*v1alpha1.Container, error)
	ListFunc        func(ctx context.Context, maxPageSize int32, pageToken string) (*v1alpha1.ContainerList, error)
	DeleteFunc      func(ctx context.Context, containerID string) error
	CheckHealthFunc func(ctx context.Context) error
}

func (m *mockContainerRepository) Create(ctx context.Context, spec v1alpha1.ContainerSpec, id string) (*v1alpha1.Container, error) {
	if m.CreateFunc == nil {
		panic("unexpected call to Create")
	}
	return m.CreateFunc(ctx, spec, id)
}

func (m *mockContainerRepository) Get(ctx context.Context, containerID string) (*v1alpha1.Container, error) {
	if m.GetFunc == nil {
		panic("unexpected call to Get")
	}
	return m.GetFunc(ctx, containerID)
}

func (m *mockContainerRepository) List(ctx context.Context, maxPageSize int32, pageToken string) (*v1alpha1.ContainerList, error) {
	if m.ListFunc == nil {
		panic("unexpected call to List")
	}
	return m.ListFunc(ctx, maxPageSize, pageToken)
}

func (m *mockContainerRepository) Delete(ctx context.Context, containerID string) error {
	if m.DeleteFunc == nil {
		panic("unexpected call to Delete")
	}
	return m.DeleteFunc(ctx, containerID)
}

func (m *mockContainerRepository) CheckHealth(ctx context.Context) error {
	if m.CheckHealthFunc == nil {
		return nil
	}
	return m.CheckHealthFunc(ctx)
}

// ---------------------------------------------------------------------------
// Test data helpers
// ---------------------------------------------------------------------------

// validCreateBody returns a ContainerSpec with all required fields populated,
// suitable for use as a CreateContainer request body.
func validCreateBody() v1alpha1.ContainerSpec {
	return v1alpha1.ContainerSpec{
		ServiceType: v1alpha1.ContainerSpecServiceTypeContainer,
		Metadata: v1alpha1.ContainerMetadata{
			Name: "my-container",
		},
		Image: v1alpha1.ContainerImage{
			Reference: "nginx:latest",
		},
		Resources: v1alpha1.ContainerResources{
			Cpu: v1alpha1.ContainerCpu{
				Min: 1,
				Max: 2,
			},
			Memory: v1alpha1.ContainerMemory{
				Min: "1GB",
				Max: "2GB",
			},
		},
	}
}

// newContainerResult simulates the enriched output the store returns after a
// successful Create. Read-only fields (id, path, status, timestamps, namespace)
// are populated as the real store would set them.
func newContainerResult(spec v1alpha1.ContainerSpec, id string) *v1alpha1.Container {
	now := time.Now().UTC()
	status := v1alpha1.PENDING
	path := "containers/" + id
	ns := testNamespace

	spec.Metadata.Namespace = &ns

	return &v1alpha1.Container{
		Id:         &id,
		Path:       &path,
		Status:     &status,
		CreateTime: &now,
		UpdateTime: &now,
		Spec:       spec,
	}
}
