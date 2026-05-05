package container_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"time"

	v1alpha1 "github.com/dcm-project/k8s-container-service-provider/api/v1alpha1"
	oapigen "github.com/dcm-project/k8s-container-service-provider/internal/api/server"
	"github.com/dcm-project/k8s-container-service-provider/internal/handlers/container"
	"github.com/dcm-project/k8s-container-service-provider/internal/store"
	"github.com/dcm-project/k8s-container-service-provider/internal/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Container API Handlers", func() {
	var (
		repo *mockContainerRepository
		h    oapigen.StrictServerInterface
	)

	BeforeEach(func() {
		repo = &mockContainerRepository{}
		h = container.NewHandler(repo, slog.New(slog.NewJSONHandler(io.Discard, nil)), time.Now(), "1.0.0")
	})

	// -----------------------------------------------------------------------
	// GetHealth
	// -----------------------------------------------------------------------
	Describe("GetHealth", func() {
		// TC-U005: Returns 200 OK with correct body when healthy
		// Validates: REQ-HLT-010, REQ-HLT-020, REQ-HLT-050
		// Transitively covers: TC-U007 (REQ-HLT-040 — GetHealth uses only
		// startTime, version, and healthChecker, never touches the store)
		It("returns 200 with correct response fields when healthy (TC-U005)", func() {
			repo := &mockContainerRepository{CheckHealthFunc: func(_ context.Context) error { return nil }}
			h := container.NewHandler(repo, slog.New(slog.NewJSONHandler(io.Discard, nil)), time.Now(), "2.3.4")

			resp, err := h.GetHealth(context.Background(), oapigen.GetHealthRequestObject{})
			Expect(err).NotTo(HaveOccurred())

			okResp, ok := resp.(oapigen.GetHealth200JSONResponse)
			Expect(ok).To(BeTrue(), "expected GetHealth200JSONResponse")

			Expect(okResp.Status).To(Equal("healthy"))
			Expect(okResp.Type).NotTo(BeNil())
			Expect(*okResp.Type).To(Equal("k8s-container-service-provider.dcm.io/health"))
			Expect(okResp.Path).NotTo(BeNil())
			Expect(*okResp.Path).To(Equal("health"))
			Expect(okResp.Version).NotTo(BeNil())
			Expect(*okResp.Version).To(Equal("2.3.4"))
			Expect(okResp.Uptime).NotTo(BeNil())
			Expect(*okResp.Uptime).To(BeNumerically(">=", 0))
		})

		// TC-U006: Uptime increases over time
		// Validates: REQ-HLT-020
		It("reports uptime increasing over time (TC-U006)", func() {
			startTime := time.Now().Add(-60 * time.Second)
			repo := &mockContainerRepository{CheckHealthFunc: func(_ context.Context) error { return nil }}
			h := container.NewHandler(repo, slog.New(slog.NewJSONHandler(io.Discard, nil)), startTime, "1.0.0")

			resp, err := h.GetHealth(context.Background(), oapigen.GetHealthRequestObject{})
			Expect(err).NotTo(HaveOccurred())

			okResp, ok := resp.(oapigen.GetHealth200JSONResponse)
			Expect(ok).To(BeTrue(), "expected GetHealth200JSONResponse")

			Expect(okResp.Uptime).NotTo(BeNil())
			Expect(*okResp.Uptime).To(BeNumerically(">=", 60))
		})

		// TC-U087: GetHealth returns "unhealthy" when HealthChecker fails
		// Validates: REQ-HLT-020, REQ-HLT-060
		It("returns unhealthy when health check fails (TC-U087)", func() {
			repo := &mockContainerRepository{CheckHealthFunc: func(_ context.Context) error { return errors.New("connection refused") }}
			h := container.NewHandler(repo, slog.New(slog.NewJSONHandler(io.Discard, nil)), time.Now(), "1.0.0")

			resp, err := h.GetHealth(context.Background(), oapigen.GetHealthRequestObject{})
			Expect(err).NotTo(HaveOccurred())

			okResp, ok := resp.(oapigen.GetHealth200JSONResponse)
			Expect(ok).To(BeTrue(), "expected GetHealth200JSONResponse")

			Expect(okResp.Status).To(Equal("unhealthy"))
		})

		// TC-U088: GetHealth returns all fields when unhealthy
		// Validates: REQ-HLT-060
		It("returns all fields when unhealthy (TC-U088)", func() {
			repo := &mockContainerRepository{CheckHealthFunc: func(_ context.Context) error { return errors.New("connection refused") }}
			h := container.NewHandler(repo, slog.New(slog.NewJSONHandler(io.Discard, nil)), time.Now(), "2.3.4")

			resp, err := h.GetHealth(context.Background(), oapigen.GetHealthRequestObject{})
			Expect(err).NotTo(HaveOccurred())

			okResp, ok := resp.(oapigen.GetHealth200JSONResponse)
			Expect(ok).To(BeTrue(), "expected GetHealth200JSONResponse")

			Expect(okResp.Status).To(Equal("unhealthy"))
			Expect(okResp.Type).NotTo(BeNil())
			Expect(*okResp.Type).To(Equal("k8s-container-service-provider.dcm.io/health"))
			Expect(okResp.Path).NotTo(BeNil())
			Expect(*okResp.Path).To(Equal("health"))
			Expect(okResp.Version).NotTo(BeNil())
			Expect(*okResp.Version).To(Equal("2.3.4"))
			Expect(okResp.Uptime).NotTo(BeNil())
			Expect(*okResp.Uptime).To(BeNumerically(">=", 0))
		})
	})

	// -----------------------------------------------------------------------
	// CreateContainer
	// -----------------------------------------------------------------------
	Describe("CreateContainer", func() {
		Context("successful creation", func() {
			// TC-U009: returns 201 with populated read-only fields
			It("returns 201 with populated read-only fields (TC-U009)", func() {
				body := validCreateBody()
				repo.CreateFunc = func(_ context.Context, c v1alpha1.ContainerSpec, id string) (*v1alpha1.Container, error) {
					return newContainerResult(c, id), nil
				}

				req := oapigen.CreateContainerRequestObject{
					Body: &v1alpha1.Container{Spec: body},
				}

				resp, err := h.CreateContainer(context.Background(), req)
				Expect(err).NotTo(HaveOccurred())

				created, ok := resp.(oapigen.CreateContainer201JSONResponse)
				Expect(ok).To(BeTrue(), "expected CreateContainer201JSONResponse")

				Expect(created.Id).NotTo(BeNil(), "id must be set")
				Expect(created.Path).NotTo(BeNil(), "path must be set")
				Expect(created.Status).NotTo(BeNil(), "status must be set")
				Expect(*created.Status).To(Equal(v1alpha1.PENDING))
				Expect(created.CreateTime).NotTo(BeNil(), "create_time must be set")
				Expect(created.UpdateTime).NotTo(BeNil(), "update_time must be set")
				Expect(created.Spec.Metadata.Namespace).NotTo(BeNil(), "namespace must be set")
				Expect(*created.Spec.Metadata.Namespace).To(Equal(testNamespace))
			})

			// TC-U010: generates UUID when no id query param
			It("generates UUID when no id query param (TC-U010)", func() {
				body := validCreateBody()
				var capturedID string
				repo.CreateFunc = func(_ context.Context, c v1alpha1.ContainerSpec, id string) (*v1alpha1.Container, error) {
					capturedID = id
					return newContainerResult(c, id), nil
				}

				req := oapigen.CreateContainerRequestObject{
					Body: &v1alpha1.Container{Spec: body},
					// Params.Id is nil — no client-specified ID
				}

				resp, err := h.CreateContainer(context.Background(), req)
				Expect(err).NotTo(HaveOccurred())

				_, ok := resp.(oapigen.CreateContainer201JSONResponse)
				Expect(ok).To(BeTrue(), "expected CreateContainer201JSONResponse")
				Expect(capturedID).NotTo(BeEmpty(), "handler must generate an ID")
			})

			// TC-U011: uses client-specified id
			It("uses client-specified id (TC-U011)", func() {
				body := validCreateBody()
				clientID := "my-custom-id"
				var capturedID string
				repo.CreateFunc = func(_ context.Context, c v1alpha1.ContainerSpec, id string) (*v1alpha1.Container, error) {
					capturedID = id
					return newContainerResult(c, id), nil
				}

				req := oapigen.CreateContainerRequestObject{
					Params: v1alpha1.CreateContainerParams{Id: util.Ptr(clientID)},
					Body:   &v1alpha1.Container{Spec: body},
				}

				resp, err := h.CreateContainer(context.Background(), req)
				Expect(err).NotTo(HaveOccurred())

				_, ok := resp.(oapigen.CreateContainer201JSONResponse)
				Expect(ok).To(BeTrue(), "expected CreateContainer201JSONResponse")
				Expect(capturedID).To(Equal(clientID))
			})

			// TC-U079: spec wrapper is unwrapped before passing to store
			It("unwraps spec envelope before calling store (TC-U079)", func() {
				body := validCreateBody()
				var capturedSpec v1alpha1.ContainerSpec
				repo.CreateFunc = func(_ context.Context, c v1alpha1.ContainerSpec, id string) (*v1alpha1.Container, error) {
					capturedSpec = c
					return newContainerResult(c, id), nil
				}

				req := oapigen.CreateContainerRequestObject{
					Body: &v1alpha1.Container{Spec: body},
				}

				resp, err := h.CreateContainer(context.Background(), req)
				Expect(err).NotTo(HaveOccurred())

				_, ok := resp.(oapigen.CreateContainer201JSONResponse)
				Expect(ok).To(BeTrue(), "expected CreateContainer201JSONResponse")
				Expect(capturedSpec.Metadata.Name).To(Equal(body.Metadata.Name))
				Expect(capturedSpec.Image.Reference).To(Equal(body.Image.Reference))
			})

			// TC-U081: provider_hints are accepted without error
			It("accepts provider_hints in container spec (TC-U081)", func() {
				body := validCreateBody()
				hints := map[string]interface{}{"gpu": true, "zone": "us-east-1"}
				body.ProviderHints = &hints
				repo.CreateFunc = func(_ context.Context, c v1alpha1.ContainerSpec, id string) (*v1alpha1.Container, error) {
					return newContainerResult(c, id), nil
				}

				req := oapigen.CreateContainerRequestObject{
					Body: &v1alpha1.Container{Spec: body},
				}

				resp, err := h.CreateContainer(context.Background(), req)
				Expect(err).NotTo(HaveOccurred())

				_, ok := resp.(oapigen.CreateContainer201JSONResponse)
				Expect(ok).To(BeTrue(), "expected CreateContainer201JSONResponse")
			})
		})

		Context("conflict handling", func() {
			// TC-U013: returns 409 on name conflict
			It("returns 409 on name conflict (TC-U013)", func() {
				body := validCreateBody()
				repo.CreateFunc = func(_ context.Context, _ v1alpha1.ContainerSpec, _ string) (*v1alpha1.Container, error) {
					return nil, &store.ConflictError{Message: body.Metadata.Name}
				}

				req := oapigen.CreateContainerRequestObject{
					Body: &v1alpha1.Container{Spec: body},
				}

				resp, err := h.CreateContainer(context.Background(), req)
				Expect(err).NotTo(HaveOccurred())

				errResp, ok := resp.(oapigen.CreateContainer409ApplicationProblemPlusJSONResponse)
				Expect(ok).To(BeTrue(), "expected 409 response")
				Expect(errResp.Type).To(Equal(v1alpha1.ALREADYEXISTS))
			})

			// TC-U046: rejects duplicate client-specified id
			It("rejects duplicate client-specified id (TC-U046)", func() {
				body := validCreateBody()
				clientID := "duplicate-id"
				repo.CreateFunc = func(_ context.Context, _ v1alpha1.ContainerSpec, _ string) (*v1alpha1.Container, error) {
					return nil, &store.ConflictError{Message: clientID}
				}

				req := oapigen.CreateContainerRequestObject{
					Params: v1alpha1.CreateContainerParams{Id: util.Ptr(clientID)},
					Body:   &v1alpha1.Container{Spec: body},
				}

				resp, err := h.CreateContainer(context.Background(), req)
				Expect(err).NotTo(HaveOccurred())

				errResp, ok := resp.(oapigen.CreateContainer409ApplicationProblemPlusJSONResponse)
				Expect(ok).To(BeTrue(), "expected 409 response")
				Expect(errResp.Type).To(Equal(v1alpha1.ALREADYEXISTS))
			})
		})

		Context("request validation", func() {
			// TC-U069: accepts and propagates non-reserved user labels
			It("accepts and propagates non-reserved user labels (TC-U069)", func() {
				body := validCreateBody()
				inputLabels := map[string]string{"team": "platform", "env": "dev"}
				body.Metadata.Labels = &inputLabels

				var capturedSpec v1alpha1.ContainerSpec
				repo.CreateFunc = func(_ context.Context, c v1alpha1.ContainerSpec, id string) (*v1alpha1.Container, error) {
					capturedSpec = c
					return newContainerResult(c, id), nil
				}

				req := oapigen.CreateContainerRequestObject{
					Body: &v1alpha1.Container{Spec: body},
				}

				resp, err := h.CreateContainer(context.Background(), req)
				Expect(err).NotTo(HaveOccurred())

				created, ok := resp.(oapigen.CreateContainer201JSONResponse)
				Expect(ok).To(BeTrue(), "expected CreateContainer201JSONResponse")

				Expect(capturedSpec.Metadata.Labels).NotTo(BeNil(), "labels must reach the store")
				Expect(*capturedSpec.Metadata.Labels).To(Equal(inputLabels))

				Expect(created.Spec.Metadata.Labels).NotTo(BeNil(), "labels must be in the response")
				Expect(*created.Spec.Metadata.Labels).To(Equal(inputLabels))
			})

			// TC-U048: rejects min > max resources
			DescribeTable("rejects min > max resources (TC-U048)",
				func(mutate func(*v1alpha1.ContainerSpec)) {
					body := validCreateBody()
					mutate(&body)

					req := oapigen.CreateContainerRequestObject{
						Body: &v1alpha1.Container{Spec: body},
					}

					resp, err := h.CreateContainer(context.Background(), req)
					Expect(err).NotTo(HaveOccurred())

					errResp, ok := resp.(oapigen.CreateContainer400ApplicationProblemPlusJSONResponse)
					Expect(ok).To(BeTrue(), "expected 400 response for min > max")
					Expect(errResp.Type).To(Equal(v1alpha1.INVALIDARGUMENT))
				},
				Entry("CPU min > max", func(c *v1alpha1.ContainerSpec) {
					c.Resources.Cpu.Min = 4
					c.Resources.Cpu.Max = 2
				}),
				Entry("memory min > max", func(c *v1alpha1.ContainerSpec) {
					c.Resources.Memory.Min = "4GB"
					c.Resources.Memory.Max = "2GB"
				}),
			)

			// TC-U054: rejects invalid memory format
			DescribeTable("rejects invalid memory format (TC-U054)",
				func(memMin, memMax string) {
					body := validCreateBody()
					body.Resources.Memory.Min = memMin
					body.Resources.Memory.Max = memMax

					req := oapigen.CreateContainerRequestObject{
						Body: &v1alpha1.Container{Spec: body},
					}

					resp, err := h.CreateContainer(context.Background(), req)
					Expect(err).NotTo(HaveOccurred())

					errResp, ok := resp.(oapigen.CreateContainer400ApplicationProblemPlusJSONResponse)
					Expect(ok).To(BeTrue(), "expected 400 response for invalid memory format")
					Expect(errResp.Type).To(Equal(v1alpha1.INVALIDARGUMENT))
				},
				Entry("unsupported unit XB in min", "10XB", "2GB"),
				Entry("unsupported unit KB in max", "1GB", "10KB"),
				Entry("no unit in min", "1024", "2GB"),
			)

			// TC-U078: rejects reserved "health" container ID
			It("rejects reserved health container ID (TC-U078)", func() {
				body := validCreateBody()
				clientID := "health"

				req := oapigen.CreateContainerRequestObject{
					Params: v1alpha1.CreateContainerParams{Id: util.Ptr(clientID)},
					Body:   &v1alpha1.Container{Spec: body},
				}

				resp, err := h.CreateContainer(context.Background(), req)
				Expect(err).NotTo(HaveOccurred())

				errResp, ok := resp.(oapigen.CreateContainer400ApplicationProblemPlusJSONResponse)
				Expect(ok).To(BeTrue(), "expected 400 response for reserved ID")
				Expect(errResp.Type).To(Equal(v1alpha1.INVALIDARGUMENT))
				Expect(*errResp.Detail).To(ContainSubstring("reserved"))
			})

			// TC-U049: rejects DCM label collision
			DescribeTable("rejects DCM label collision (TC-U049)",
				func(label string) {
					body := validCreateBody()
					body.Metadata.Labels = &map[string]string{label: "user-value"}

					req := oapigen.CreateContainerRequestObject{
						Body: &v1alpha1.Container{Spec: body},
					}

					resp, err := h.CreateContainer(context.Background(), req)
					Expect(err).NotTo(HaveOccurred())

					errResp, ok := resp.(oapigen.CreateContainer400ApplicationProblemPlusJSONResponse)
					Expect(ok).To(BeTrue(), "expected 400 for reserved label %q", label)
					Expect(errResp.Type).To(Equal(v1alpha1.INVALIDARGUMENT))
				},
				Entry("dcm.project/managed-by", "dcm.project/managed-by"),
				Entry("dcm.project/dcm-instance-id", "dcm.project/dcm-instance-id"),
				Entry("dcm.project/dcm-service-type", "dcm.project/dcm-service-type"),
			)
		})
	})

	// -----------------------------------------------------------------------
	// ListContainers
	// -----------------------------------------------------------------------
	Describe("ListContainers", func() {
		// TC-U015: returns 200 with containers
		It("returns 200 with containers (TC-U015)", func() {
			c1 := *newContainerResult(validCreateBody(), "id-1")
			c2 := *newContainerResult(validCreateBody(), "id-2")
			repo.ListFunc = func(_ context.Context, _ int32, _ string) (*v1alpha1.ContainerList, error) {
				return &v1alpha1.ContainerList{
					Containers: &[]v1alpha1.Container{c1, c2},
				}, nil
			}

			req := oapigen.ListContainersRequestObject{}

			resp, err := h.ListContainers(context.Background(), req)
			Expect(err).NotTo(HaveOccurred())

			listResp, ok := resp.(oapigen.ListContainers200JSONResponse)
			Expect(ok).To(BeTrue(), "expected ListContainers200JSONResponse")
			Expect(listResp.Containers).NotTo(BeNil())
			Expect(*listResp.Containers).To(HaveLen(2))
		})

		// TC-U016: supports pagination parameters
		It("supports pagination parameters (TC-U016)", func() {
			var capturedPageSize int32
			var capturedPageToken string
			repo.ListFunc = func(_ context.Context, maxPageSize int32, pageToken string) (*v1alpha1.ContainerList, error) {
				capturedPageSize = maxPageSize
				capturedPageToken = pageToken
				return &v1alpha1.ContainerList{
					Containers:    &[]v1alpha1.Container{},
					NextPageToken: util.Ptr("next-token"),
				}, nil
			}

			req := oapigen.ListContainersRequestObject{
				Params: v1alpha1.ListContainersParams{
					MaxPageSize: util.Ptr(int32(10)),
					PageToken:   util.Ptr("page-1"),
				},
			}

			resp, err := h.ListContainers(context.Background(), req)
			Expect(err).NotTo(HaveOccurred())

			listResp, ok := resp.(oapigen.ListContainers200JSONResponse)
			Expect(ok).To(BeTrue(), "expected ListContainers200JSONResponse")
			Expect(capturedPageSize).To(Equal(int32(10)))
			Expect(capturedPageToken).To(Equal("page-1"))
			Expect(listResp.NextPageToken).To(Equal(util.Ptr("next-token")))
		})

		// TC-U017: returns empty array when no containers
		It("returns empty array when no containers (TC-U017)", func() {
			repo.ListFunc = func(_ context.Context, _ int32, _ string) (*v1alpha1.ContainerList, error) {
				return &v1alpha1.ContainerList{
					Containers: &[]v1alpha1.Container{},
				}, nil
			}

			req := oapigen.ListContainersRequestObject{}

			resp, err := h.ListContainers(context.Background(), req)
			Expect(err).NotTo(HaveOccurred())

			listResp, ok := resp.(oapigen.ListContainers200JSONResponse)
			Expect(ok).To(BeTrue(), "expected ListContainers200JSONResponse")
			Expect(listResp.Containers).NotTo(BeNil())
			Expect(*listResp.Containers).To(BeEmpty())
		})

		// TC-U050: rejects invalid page_token
		It("rejects invalid page_token (TC-U050)", func() {
			repo.ListFunc = func(_ context.Context, _ int32, _ string) (*v1alpha1.ContainerList, error) {
				return nil, &store.InvalidArgumentError{Message: "invalid page token"}
			}

			req := oapigen.ListContainersRequestObject{
				Params: v1alpha1.ListContainersParams{
					PageToken: util.Ptr("invalid-token"),
				},
			}

			resp, err := h.ListContainers(context.Background(), req)
			Expect(err).NotTo(HaveOccurred())

			errResp, ok := resp.(oapigen.ListContainers400ApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue(), "expected 400 response for invalid page token")
			Expect(errResp.Type).To(Equal(v1alpha1.INVALIDARGUMENT))
		})
	})

	// -----------------------------------------------------------------------
	// GetContainer
	// -----------------------------------------------------------------------
	Describe("GetContainer", func() {
		// TC-U018: returns 200 for existing container
		It("returns 200 for existing container (TC-U018)", func() {
			result := newContainerResult(validCreateBody(), "existing-id")
			repo.GetFunc = func(_ context.Context, containerID string) (*v1alpha1.Container, error) {
				Expect(containerID).To(Equal("existing-id"))
				return result, nil
			}

			req := oapigen.GetContainerRequestObject{
				ContainerId: "existing-id",
			}

			resp, err := h.GetContainer(context.Background(), req)
			Expect(err).NotTo(HaveOccurred())

			getResp, ok := resp.(oapigen.GetContainer200JSONResponse)
			Expect(ok).To(BeTrue(), "expected GetContainer200JSONResponse")
			Expect(getResp.Id).NotTo(BeNil())
			Expect(*getResp.Id).To(Equal("existing-id"))
		})

		// TC-U019: returns 404 for non-existent container
		It("returns 404 for non-existent container (TC-U019)", func() {
			repo.GetFunc = func(_ context.Context, containerID string) (*v1alpha1.Container, error) {
				return nil, &store.NotFoundError{ID: containerID}
			}

			req := oapigen.GetContainerRequestObject{
				ContainerId: "non-existent",
			}

			resp, err := h.GetContainer(context.Background(), req)
			Expect(err).NotTo(HaveOccurred())

			errResp, ok := resp.(oapigen.GetContainer404ApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue(), "expected 404 response")
			Expect(errResp.Type).To(Equal(v1alpha1.NOTFOUND))
		})
	})

	// -----------------------------------------------------------------------
	// DeleteContainer
	// -----------------------------------------------------------------------
	Describe("DeleteContainer", func() {
		// TC-U020: returns 204 for existing container
		It("returns 204 for existing container (TC-U020)", func() {
			repo.DeleteFunc = func(_ context.Context, containerID string) error {
				Expect(containerID).To(Equal("existing-id"))
				return nil
			}

			req := oapigen.DeleteContainerRequestObject{
				ContainerId: "existing-id",
			}

			resp, err := h.DeleteContainer(context.Background(), req)
			Expect(err).NotTo(HaveOccurred())

			_, ok := resp.(oapigen.DeleteContainer204Response)
			Expect(ok).To(BeTrue(), "expected DeleteContainer204Response")
		})

		// TC-U021: returns 404 for non-existent container
		It("returns 404 for non-existent container (TC-U021)", func() {
			repo.DeleteFunc = func(_ context.Context, containerID string) error {
				return &store.NotFoundError{ID: containerID}
			}

			req := oapigen.DeleteContainerRequestObject{
				ContainerId: "non-existent",
			}

			resp, err := h.DeleteContainer(context.Background(), req)
			Expect(err).NotTo(HaveOccurred())

			errResp, ok := resp.(oapigen.DeleteContainer404ApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue(), "expected 404 response")
			Expect(errResp.Type).To(Equal(v1alpha1.NOTFOUND))
		})
	})

	// -----------------------------------------------------------------------
	// Instance field in error responses
	// -----------------------------------------------------------------------
	Describe("Instance field", func() {
		// TC-U071: handler error responses include instance field
		DescribeTable("error responses include instance field (TC-U071)",
			func(
				setup func(),
				callHandler func(oapigen.StrictServerInterface) (interface{}, error),
				expectedInstance string,
				assertInstance func(interface{}, string),
			) {
				setup()
				resp, err := callHandler(h)
				Expect(err).NotTo(HaveOccurred())
				assertInstance(resp, expectedInstance)
			},

			Entry("CreateContainer 409 (conflict)",
				func() {
					repo.CreateFunc = func(_ context.Context, _ v1alpha1.ContainerSpec, _ string) (*v1alpha1.Container, error) {
						return nil, &store.ConflictError{Message: "dup"}
					}
				},
				func(s oapigen.StrictServerInterface) (interface{}, error) {
					body := validCreateBody()
					return s.CreateContainer(context.Background(), oapigen.CreateContainerRequestObject{Body: &v1alpha1.Container{Spec: body}})
				},
				"/api/v1alpha1/containers",
				func(resp interface{}, expected string) {
					errResp, ok := resp.(oapigen.CreateContainer409ApplicationProblemPlusJSONResponse)
					Expect(ok).To(BeTrue())
					Expect(errResp.Instance).NotTo(BeNil(), "instance must be set")
					Expect(*errResp.Instance).To(Equal(expected))
				},
			),

			Entry("GetContainer 404 (not found)",
				func() {
					repo.GetFunc = func(_ context.Context, id string) (*v1alpha1.Container, error) {
						return nil, &store.NotFoundError{ID: id}
					}
				},
				func(s oapigen.StrictServerInterface) (interface{}, error) {
					return s.GetContainer(context.Background(), oapigen.GetContainerRequestObject{ContainerId: "test-id"})
				},
				"/api/v1alpha1/containers/test-id",
				func(resp interface{}, expected string) {
					errResp, ok := resp.(oapigen.GetContainer404ApplicationProblemPlusJSONResponse)
					Expect(ok).To(BeTrue())
					Expect(errResp.Instance).NotTo(BeNil(), "instance must be set")
					Expect(*errResp.Instance).To(Equal(expected))
				},
			),

			Entry("DeleteContainer 404 (not found)",
				func() {
					repo.DeleteFunc = func(_ context.Context, id string) error {
						return &store.NotFoundError{ID: id}
					}
				},
				func(s oapigen.StrictServerInterface) (interface{}, error) {
					return s.DeleteContainer(context.Background(), oapigen.DeleteContainerRequestObject{ContainerId: "test-id"})
				},
				"/api/v1alpha1/containers/test-id",
				func(resp interface{}, expected string) {
					errResp, ok := resp.(oapigen.DeleteContainer404ApplicationProblemPlusJSONResponse)
					Expect(ok).To(BeTrue())
					Expect(errResp.Instance).NotTo(BeNil(), "instance must be set")
					Expect(*errResp.Instance).To(Equal(expected))
				},
			),

			Entry("ListContainers 400 (invalid page token)",
				func() {
					repo.ListFunc = func(_ context.Context, _ int32, _ string) (*v1alpha1.ContainerList, error) {
						return nil, &store.InvalidArgumentError{Message: "bad token"}
					}
				},
				func(s oapigen.StrictServerInterface) (interface{}, error) {
					return s.ListContainers(context.Background(), oapigen.ListContainersRequestObject{
						Params: v1alpha1.ListContainersParams{PageToken: util.Ptr("bad")},
					})
				},
				"/api/v1alpha1/containers",
				func(resp interface{}, expected string) {
					errResp, ok := resp.(oapigen.ListContainers400ApplicationProblemPlusJSONResponse)
					Expect(ok).To(BeTrue())
					Expect(errResp.Instance).NotTo(BeNil(), "instance must be set")
					Expect(*errResp.Instance).To(Equal(expected))
				},
			),

			Entry("CreateContainer 500 (internal error)",
				func() {
					repo.CreateFunc = func(_ context.Context, _ v1alpha1.ContainerSpec, _ string) (*v1alpha1.Container, error) {
						return nil, errors.New("unexpected")
					}
				},
				func(s oapigen.StrictServerInterface) (interface{}, error) {
					body := validCreateBody()
					return s.CreateContainer(context.Background(), oapigen.CreateContainerRequestObject{Body: &v1alpha1.Container{Spec: body}})
				},
				"/api/v1alpha1/containers",
				func(resp interface{}, expected string) {
					errResp, ok := resp.(oapigen.CreateContainer500ApplicationProblemPlusJSONResponse)
					Expect(ok).To(BeTrue())
					Expect(errResp.Instance).NotTo(BeNil(), "instance must be set")
					Expect(*errResp.Instance).To(Equal(expected))
				},
			),
		)
	})

	// -----------------------------------------------------------------------
	// Error handling
	// -----------------------------------------------------------------------
	Describe("Error handling", func() {
		// TC-U022: error responses use RFC 7807 format
		DescribeTable("error responses use RFC 7807 format (TC-U022)",
			func(
				callHandler func(oapigen.StrictServerInterface) (interface{}, error),
				assertError func(interface{}),
			) {
				resp, err := callHandler(h)
				Expect(err).NotTo(HaveOccurred())
				assertError(resp)
			},

			// 404 from GetContainer
			Entry("GetContainer 404 has Type and Title",
				func(s oapigen.StrictServerInterface) (interface{}, error) {
					repo.GetFunc = func(_ context.Context, id string) (*v1alpha1.Container, error) {
						return nil, &store.NotFoundError{ID: id}
					}
					return s.GetContainer(context.Background(), oapigen.GetContainerRequestObject{ContainerId: "missing"})
				},
				func(resp interface{}) {
					errResp, ok := resp.(oapigen.GetContainer404ApplicationProblemPlusJSONResponse)
					Expect(ok).To(BeTrue())
					Expect(errResp.Type).To(Equal(v1alpha1.NOTFOUND))
					Expect(errResp.Title).NotTo(BeEmpty())
				},
			),

			// 409 from CreateContainer
			Entry("CreateContainer 409 has Type and Title",
				func(s oapigen.StrictServerInterface) (interface{}, error) {
					body := validCreateBody()
					repo.CreateFunc = func(_ context.Context, _ v1alpha1.ContainerSpec, _ string) (*v1alpha1.Container, error) {
						return nil, &store.ConflictError{Message: "dup"}
					}
					return s.CreateContainer(context.Background(), oapigen.CreateContainerRequestObject{Body: &v1alpha1.Container{Spec: body}})
				},
				func(resp interface{}) {
					errResp, ok := resp.(oapigen.CreateContainer409ApplicationProblemPlusJSONResponse)
					Expect(ok).To(BeTrue())
					Expect(errResp.Type).To(Equal(v1alpha1.ALREADYEXISTS))
					Expect(errResp.Title).NotTo(BeEmpty())
				},
			),

			// 404 from DeleteContainer
			Entry("DeleteContainer 404 has Type and Title",
				func(s oapigen.StrictServerInterface) (interface{}, error) {
					repo.DeleteFunc = func(_ context.Context, id string) error {
						return &store.NotFoundError{ID: id}
					}
					return s.DeleteContainer(context.Background(), oapigen.DeleteContainerRequestObject{ContainerId: "missing"})
				},
				func(resp interface{}) {
					errResp, ok := resp.(oapigen.DeleteContainer404ApplicationProblemPlusJSONResponse)
					Expect(ok).To(BeTrue())
					Expect(errResp.Type).To(Equal(v1alpha1.NOTFOUND))
					Expect(errResp.Title).NotTo(BeEmpty())
				},
			),

			// 400 from CreateContainer (cpu min > max)
			Entry("CreateContainer 400 has Type and Title",
				func(s oapigen.StrictServerInterface) (interface{}, error) {
					body := validCreateBody()
					body.Resources.Cpu.Min = 4
					body.Resources.Cpu.Max = 2
					return s.CreateContainer(context.Background(), oapigen.CreateContainerRequestObject{
						Body: &v1alpha1.Container{Spec: body},
					})
				},
				func(resp interface{}) {
					errResp, ok := resp.(oapigen.CreateContainer400ApplicationProblemPlusJSONResponse)
					Expect(ok).To(BeTrue())
					Expect(errResp.Type).To(Equal(v1alpha1.INVALIDARGUMENT))
					Expect(errResp.Title).NotTo(BeEmpty())
				},
			),
		)

		// TC-U023: error types map to correct HTTP status codes
		DescribeTable("error types map to correct HTTP status codes (TC-U023)",
			func(
				callHandler func(oapigen.StrictServerInterface) (interface{}, error),
				expectedType interface{},
			) {
				resp, err := callHandler(h)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).To(BeAssignableToTypeOf(expectedType))
			},

			Entry("NotFoundError → 404 (Get)",
				func(s oapigen.StrictServerInterface) (interface{}, error) {
					repo.GetFunc = func(_ context.Context, id string) (*v1alpha1.Container, error) {
						return nil, &store.NotFoundError{ID: id}
					}
					return s.GetContainer(context.Background(), oapigen.GetContainerRequestObject{ContainerId: "x"})
				},
				oapigen.GetContainer404ApplicationProblemPlusJSONResponse{},
			),

			Entry("NotFoundError → 404 (Delete)",
				func(s oapigen.StrictServerInterface) (interface{}, error) {
					repo.DeleteFunc = func(_ context.Context, id string) error {
						return &store.NotFoundError{ID: id}
					}
					return s.DeleteContainer(context.Background(), oapigen.DeleteContainerRequestObject{ContainerId: "x"})
				},
				oapigen.DeleteContainer404ApplicationProblemPlusJSONResponse{},
			),

			Entry("ConflictError → 409 (Create)",
				func(s oapigen.StrictServerInterface) (interface{}, error) {
					body := validCreateBody()
					repo.CreateFunc = func(_ context.Context, _ v1alpha1.ContainerSpec, _ string) (*v1alpha1.Container, error) {
						return nil, &store.ConflictError{Message: "dup"}
					}
					return s.CreateContainer(context.Background(), oapigen.CreateContainerRequestObject{Body: &v1alpha1.Container{Spec: body}})
				},
				oapigen.CreateContainer409ApplicationProblemPlusJSONResponse{},
			),

			Entry("InvalidArgumentError → 400 (Create via store)",
				func(s oapigen.StrictServerInterface) (interface{}, error) {
					body := validCreateBody()
					repo.CreateFunc = func(_ context.Context, _ v1alpha1.ContainerSpec, _ string) (*v1alpha1.Container, error) {
						return nil, &store.InvalidArgumentError{Message: "service creation requires at least one port"}
					}
					return s.CreateContainer(context.Background(), oapigen.CreateContainerRequestObject{Body: &v1alpha1.Container{Spec: body}})
				},
				oapigen.CreateContainer400ApplicationProblemPlusJSONResponse{},
			),

			Entry("InvalidArgumentError → 400 (List)",
				func(s oapigen.StrictServerInterface) (interface{}, error) {
					repo.ListFunc = func(_ context.Context, _ int32, _ string) (*v1alpha1.ContainerList, error) {
						return nil, &store.InvalidArgumentError{Message: "bad token"}
					}
					return s.ListContainers(context.Background(), oapigen.ListContainersRequestObject{
						Params: v1alpha1.ListContainersParams{PageToken: util.Ptr("bad")},
					})
				},
				oapigen.ListContainers400ApplicationProblemPlusJSONResponse{},
			),

			Entry("unexpected error → 500 (Create)",
				func(s oapigen.StrictServerInterface) (interface{}, error) {
					body := validCreateBody()
					repo.CreateFunc = func(_ context.Context, _ v1alpha1.ContainerSpec, _ string) (*v1alpha1.Container, error) {
						return nil, errors.New("unexpected")
					}
					return s.CreateContainer(context.Background(), oapigen.CreateContainerRequestObject{Body: &v1alpha1.Container{Spec: body}})
				},
				oapigen.CreateContainer500ApplicationProblemPlusJSONResponse{},
			),

			Entry("unexpected error → 500 (Get)",
				func(s oapigen.StrictServerInterface) (interface{}, error) {
					repo.GetFunc = func(_ context.Context, _ string) (*v1alpha1.Container, error) {
						return nil, errors.New("unexpected")
					}
					return s.GetContainer(context.Background(), oapigen.GetContainerRequestObject{ContainerId: "x"})
				},
				oapigen.GetContainer500ApplicationProblemPlusJSONResponse{},
			),

			Entry("unexpected error → 500 (Delete)",
				func(s oapigen.StrictServerInterface) (interface{}, error) {
					repo.DeleteFunc = func(_ context.Context, _ string) error {
						return errors.New("unexpected")
					}
					return s.DeleteContainer(context.Background(), oapigen.DeleteContainerRequestObject{ContainerId: "x"})
				},
				oapigen.DeleteContainer500ApplicationProblemPlusJSONResponse{},
			),

			Entry("unexpected error → 500 (List)",
				func(s oapigen.StrictServerInterface) (interface{}, error) {
					repo.ListFunc = func(_ context.Context, _ int32, _ string) (*v1alpha1.ContainerList, error) {
						return nil, errors.New("unexpected")
					}
					return s.ListContainers(context.Background(), oapigen.ListContainersRequestObject{})
				},
				oapigen.ListContainers500ApplicationProblemPlusJSONResponse{},
			),
		)

		// TC-U051: returns 500 INTERNAL for unexpected store errors (all operations)
		DescribeTable("returns 500 INTERNAL for unexpected store errors (TC-U051)",
			func(
				setup func(),
				callHandler func(oapigen.StrictServerInterface) (interface{}, error),
				assert500 func(interface{}),
			) {
				setup()
				resp, err := callHandler(h)
				Expect(err).NotTo(HaveOccurred())
				assert500(resp)
			},

			Entry("Create",
				func() {
					repo.CreateFunc = func(_ context.Context, _ v1alpha1.ContainerSpec, _ string) (*v1alpha1.Container, error) {
						return nil, errors.New("database connection lost")
					}
				},
				func(s oapigen.StrictServerInterface) (interface{}, error) {
					body := validCreateBody()
					return s.CreateContainer(context.Background(), oapigen.CreateContainerRequestObject{Body: &v1alpha1.Container{Spec: body}})
				},
				func(resp interface{}) {
					errResp, ok := resp.(oapigen.CreateContainer500ApplicationProblemPlusJSONResponse)
					Expect(ok).To(BeTrue(), "expected 500 response")
					Expect(errResp.Type).To(Equal(v1alpha1.INTERNAL))
					Expect(*errResp.Detail).To(Equal("an unexpected error occurred"))
					Expect(*errResp.Detail).NotTo(ContainSubstring("database connection lost"))
				},
			),

			Entry("Get",
				func() {
					repo.GetFunc = func(_ context.Context, _ string) (*v1alpha1.Container, error) {
						return nil, errors.New("connection refused")
					}
				},
				func(s oapigen.StrictServerInterface) (interface{}, error) {
					return s.GetContainer(context.Background(), oapigen.GetContainerRequestObject{ContainerId: "some-id"})
				},
				func(resp interface{}) {
					errResp, ok := resp.(oapigen.GetContainer500ApplicationProblemPlusJSONResponse)
					Expect(ok).To(BeTrue(), "expected 500 response")
					Expect(errResp.Type).To(Equal(v1alpha1.INTERNAL))
					Expect(*errResp.Detail).To(Equal("an unexpected error occurred"))
					Expect(*errResp.Detail).NotTo(ContainSubstring("connection refused"))
				},
			),

			Entry("Delete",
				func() {
					repo.DeleteFunc = func(_ context.Context, _ string) error {
						return errors.New("disk I/O error")
					}
				},
				func(s oapigen.StrictServerInterface) (interface{}, error) {
					return s.DeleteContainer(context.Background(), oapigen.DeleteContainerRequestObject{ContainerId: "some-id"})
				},
				func(resp interface{}) {
					errResp, ok := resp.(oapigen.DeleteContainer500ApplicationProblemPlusJSONResponse)
					Expect(ok).To(BeTrue(), "expected 500 response")
					Expect(errResp.Type).To(Equal(v1alpha1.INTERNAL))
					Expect(*errResp.Detail).To(Equal("an unexpected error occurred"))
					Expect(*errResp.Detail).NotTo(ContainSubstring("disk I/O error"))
				},
			),

			Entry("List",
				func() {
					repo.ListFunc = func(_ context.Context, _ int32, _ string) (*v1alpha1.ContainerList, error) {
						return nil, errors.New("network timeout")
					}
				},
				func(s oapigen.StrictServerInterface) (interface{}, error) {
					return s.ListContainers(context.Background(), oapigen.ListContainersRequestObject{})
				},
				func(resp interface{}) {
					errResp, ok := resp.(oapigen.ListContainers500ApplicationProblemPlusJSONResponse)
					Expect(ok).To(BeTrue(), "expected 500 response")
					Expect(errResp.Type).To(Equal(v1alpha1.INTERNAL))
					Expect(*errResp.Detail).To(Equal("an unexpected error occurred"))
					Expect(*errResp.Detail).NotTo(ContainSubstring("network timeout"))
				},
			),
		)
	})
})
