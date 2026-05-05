package apiserver_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	v1alpha1 "github.com/dcm-project/k8s-container-service-provider/api/v1alpha1"
	oapigen "github.com/dcm-project/k8s-container-service-provider/internal/api/server"
	"github.com/dcm-project/k8s-container-service-provider/internal/apiserver"
	"github.com/dcm-project/k8s-container-service-provider/internal/config"
	"github.com/dcm-project/k8s-container-service-provider/internal/handlers/container"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// stubContainerRepository is a minimal store.ContainerRepository for TC-U067.
// Only Create is implemented; other methods panic if called unexpectedly.
type stubContainerRepository struct{}

func (s *stubContainerRepository) Create(_ context.Context, spec v1alpha1.ContainerSpec, id string) (*v1alpha1.Container, error) {
	now := time.Now().UTC()
	status := v1alpha1.PENDING
	path := "containers/" + id
	ns := "default"
	spec.Metadata.Namespace = &ns
	return &v1alpha1.Container{
		Id:         &id,
		Path:       &path,
		Status:     &status,
		CreateTime: &now,
		UpdateTime: &now,
		Spec:       spec,
	}, nil
}

func (s *stubContainerRepository) Get(_ context.Context, _ string) (*v1alpha1.Container, error) {
	panic("unexpected call to Get")
}

func (s *stubContainerRepository) List(_ context.Context, _ int32, _ string) (*v1alpha1.ContainerList, error) {
	panic("unexpected call to List")
}

func (s *stubContainerRepository) Delete(_ context.Context, _ string) error {
	panic("unexpected call to Delete")
}

func (s *stubContainerRepository) CheckHealth(_ context.Context) error {
	return nil
}

var _ = Describe("Container API Handlers - Request Validation", func() {
	// startValidationServer starts a minimal server for validation tests and
	// returns the base URL. The server is stopped when the test context ends.
	startValidationServer := func() string {
		cfg := &config.Config{
			Server: config.ServerConfig{
				Address:         ":0",
				ShutdownTimeout: 5 * time.Second,
			},
		}
		logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
		ch := container.NewHandler(nil, logger, time.Now(), "0.0.1-test")
		h := oapigen.NewStrictHandlerWithOptions(ch, nil, oapigen.StrictHTTPServerOptions{})
		srv := apiserver.New(cfg, logger, h)

		ln, err := net.Listen("tcp", ":0")
		Expect(err).NotTo(HaveOccurred())
		addr := ln.Addr().String()

		ctx, cancel := context.WithCancel(context.Background())
		DeferCleanup(cancel)

		errCh := make(chan error, 1)
		go func() {
			errCh <- srv.Run(ctx, ln)
		}()

		// Wait for server to be ready.
		Eventually(func() error {
			resp, reqErr := http.Get(fmt.Sprintf("http://%s/api/v1alpha1/containers/health", addr))
			if reqErr != nil {
				return reqErr
			}
			_ = resp.Body.Close()
			return nil
		}).WithTimeout(5 * time.Second).WithPolling(50 * time.Millisecond).Should(Succeed())

		return fmt.Sprintf("http://%s", addr)
	}

	// TC-U014: validates request body via OpenAPI middleware
	DescribeTable("validates request body via OpenAPI middleware (TC-U014)",
		func(bodyJSON string, description string) {
			baseURL := startValidationServer()

			resp, err := http.Post(
				baseURL+"/api/v1alpha1/containers",
				"application/json",
				strings.NewReader(bodyJSON),
			)
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = resp.Body.Close() }()

			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest),
				"expected 400 for: %s", description)
			Expect(resp.Header.Get("Content-Type")).To(Equal("application/problem+json"),
				"expected RFC 7807 content type for: %s", description)

			body, err := io.ReadAll(resp.Body)
			Expect(err).NotTo(HaveOccurred())

			var problemJSON map[string]any
			Expect(json.Unmarshal(body, &problemJSON)).To(Succeed(),
				"body should be valid JSON for: %s", description)
			Expect(problemJSON).To(HaveKey("type"),
				"RFC 7807 body must have 'type' for: %s", description)
			Expect(problemJSON["type"]).To(Equal("INVALID_ARGUMENT"))
			Expect(problemJSON).To(HaveKey("title"),
				"RFC 7807 body must have 'title' for: %s", description)
			Expect(problemJSON).To(HaveKey("status"),
				"RFC 7807 body must have 'status' for: %s", description)
		},

		// Missing spec wrapper
		Entry("empty object",
			`{}`,
			"empty object missing required spec field"),

		// Missing required top-level fields inside spec
		Entry("missing image",
			`{"spec":{"service_type":"container","metadata":{"name":"test"},"resources":{"cpu":{"min":1,"max":2},"memory":{"min":"1GB","max":"2GB"}}}}`,
			"missing required image field"),
		Entry("missing metadata",
			`{"spec":{"service_type":"container","image":{"reference":"nginx:latest"},"resources":{"cpu":{"min":1,"max":2},"memory":{"min":"1GB","max":"2GB"}}}}`,
			"missing required metadata field"),
		Entry("missing resources",
			`{"spec":{"service_type":"container","image":{"reference":"nginx:latest"},"metadata":{"name":"test"}}}`,
			"missing required resources field"),
		Entry("missing service_type",
			`{"spec":{"image":{"reference":"nginx:latest"},"metadata":{"name":"test"},"resources":{"cpu":{"min":1,"max":2},"memory":{"min":"1GB","max":"2GB"}}}}`,
			"missing required service_type field"),

		// Missing required nested fields
		Entry("missing metadata.name",
			`{"spec":{"service_type":"container","image":{"reference":"nginx:latest"},"metadata":{},"resources":{"cpu":{"min":1,"max":2},"memory":{"min":"1GB","max":"2GB"}}}}`,
			"missing required metadata.name"),
		Entry("missing image.reference",
			`{"spec":{"service_type":"container","image":{},"metadata":{"name":"test"},"resources":{"cpu":{"min":1,"max":2},"memory":{"min":"1GB","max":"2GB"}}}}`,
			"missing required image.reference"),
		Entry("missing resources.cpu",
			`{"spec":{"service_type":"container","image":{"reference":"nginx:latest"},"metadata":{"name":"test"},"resources":{"memory":{"min":"1GB","max":"2GB"}}}}`,
			"missing required resources.cpu"),
		Entry("missing resources.memory",
			`{"spec":{"service_type":"container","image":{"reference":"nginx:latest"},"metadata":{"name":"test"},"resources":{"cpu":{"min":1,"max":2}}}}`,
			"missing required resources.memory"),

		// Invalid types
		Entry("invalid service_type enum",
			`{"spec":{"service_type":"invalid","image":{"reference":"nginx:latest"},"metadata":{"name":"test"},"resources":{"cpu":{"min":1,"max":2},"memory":{"min":"1GB","max":"2GB"}}}}`,
			"invalid service_type enum value"),
		Entry("cpu.min is string instead of int",
			`{"spec":{"service_type":"container","image":{"reference":"nginx:latest"},"metadata":{"name":"test"},"resources":{"cpu":{"min":"one","max":2},"memory":{"min":"1GB","max":"2GB"}}}}`,
			"cpu.min wrong type"),
		Entry("cpu.max is negative",
			`{"spec":{"service_type":"container","image":{"reference":"nginx:latest"},"metadata":{"name":"test"},"resources":{"cpu":{"min":1,"max":-1},"memory":{"min":"1GB","max":"2GB"}}}}`,
			"cpu.max negative value"),

		// TC-U055: cpu.min=0 rejected by OpenAPI minimum: 1
		Entry("cpu.min is 0 (TC-U055)",
			`{"spec":{"service_type":"container","image":{"reference":"nginx:latest"},"metadata":{"name":"test"},"resources":{"cpu":{"min":0,"max":2},"memory":{"min":"1GB","max":"2GB"}}}}`,
			"cpu.min below minimum 1"),

		// Malformed JSON
		Entry("malformed JSON",
			`{not valid json}`,
			"malformed JSON body"),

		// Missing body entirely (empty string)
		Entry("empty body",
			``,
			"empty request body"),

		// Invalid nested object structure
		Entry("network.ports is string instead of array",
			`{"spec":{"service_type":"container","image":{"reference":"nginx:latest"},"metadata":{"name":"test"},"resources":{"cpu":{"min":1,"max":2},"memory":{"min":"1GB","max":"2GB"}},"network":{"ports":"invalid"}}}`,
			"network.ports wrong type"),
		Entry("missing cpu.min",
			`{"spec":{"service_type":"container","image":{"reference":"nginx:latest"},"metadata":{"name":"test"},"resources":{"cpu":{"max":2},"memory":{"min":"1GB","max":"2GB"}}}}`,
			"missing required cpu.min"),

		// TC-U053: invalid metadata.name format
		Entry("metadata.name with invalid characters (TC-U053)",
			`{"spec":{"service_type":"container","image":{"reference":"nginx:latest"},"metadata":{"name":"Invalid_Name!"},"resources":{"cpu":{"min":1,"max":2},"memory":{"min":"1GB","max":"2GB"}}}}`,
			"metadata.name with invalid characters"),

		// TC-U056: port out of range
		Entry("container_port exceeds 65535 (TC-U056)",
			`{"spec":{"service_type":"container","image":{"reference":"nginx:latest"},"metadata":{"name":"test"},"resources":{"cpu":{"min":1,"max":2},"memory":{"min":"1GB","max":"2GB"}},"network":{"ports":[{"container_port":70000}]}}}`,
			"container_port above maximum 65535"),
		Entry("container_port is 0 (TC-U056)",
			`{"spec":{"service_type":"container","image":{"reference":"nginx:latest"},"metadata":{"name":"test"},"resources":{"cpu":{"min":1,"max":2},"memory":{"min":"1GB","max":"2GB"}},"network":{"ports":[{"container_port":0}]}}}`,
			"container_port below minimum 1"),

		// TC-U080: raw Container body without spec wrapper rejected
		Entry("raw Container body without spec wrapper (TC-U080)",
			`{"service_type":"container","image":{"reference":"nginx:latest"},"metadata":{"name":"test"},"resources":{"cpu":{"min":1,"max":2},"memory":{"min":"1GB","max":"2GB"}}}`,
			"raw Container body missing required spec wrapper"),
	)

	// TC-U012: rejects invalid client IDs via OpenAPI middleware
	DescribeTable("rejects invalid client IDs (TC-U012)",
		func(invalidID string, description string) {
			baseURL := startValidationServer()

			body := `{"spec":{"service_type":"container","metadata":{"name":"test"},"image":{"reference":"nginx:latest"},"resources":{"cpu":{"min":1,"max":2},"memory":{"min":"1GB","max":"2GB"}}}}`
			resp, err := http.Post(
				baseURL+"/api/v1alpha1/containers?id="+invalidID,
				"application/json",
				strings.NewReader(body),
			)
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = resp.Body.Close() }()

			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest),
				"expected 400 for: %s", description)
			Expect(resp.Header.Get("Content-Type")).To(Equal("application/problem+json"),
				"expected RFC 7807 content type for: %s", description)

			respBody, err := io.ReadAll(resp.Body)
			Expect(err).NotTo(HaveOccurred())

			var problemJSON map[string]any
			Expect(json.Unmarshal(respBody, &problemJSON)).To(Succeed())
			Expect(problemJSON["type"]).To(Equal("INVALID_ARGUMENT"))
		},
		Entry("leading dash", "-leading-dash", "ID starting with dash"),
		Entry("trailing dash", "trailing-", "ID ending with dash"),
		Entry("has underscore", "has_underscore", "ID containing underscore"),
		Entry("UPPERCASE", "UPPERCASE", "ID with uppercase letters"),
		Entry("too long (64 chars)", strings.Repeat("a", 64), "ID exceeding 63 character limit"),
	)

	// TC-U047: accepts valid boundary IDs via OpenAPI middleware
	DescribeTable("accepts valid boundary IDs (TC-U047)",
		func(validID string, description string) {
			baseURL := startValidationServer()

			body := `{"spec":{"service_type":"container","metadata":{"name":"test"},"image":{"reference":"nginx:latest"},"resources":{"cpu":{"min":1,"max":2},"memory":{"min":"1GB","max":"2GB"}}}}`
			resp, err := http.Post(
				baseURL+"/api/v1alpha1/containers?id="+validID,
				"application/json",
				strings.NewReader(body),
			)
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = resp.Body.Close() }()

			// Valid ID should pass middleware validation and reach the handler
			// (not be rejected with 400 by OpenAPI middleware).
			Expect(resp.StatusCode).NotTo(Equal(http.StatusBadRequest),
				"valid ID should pass OpenAPI validation: %s", description)
		},
		Entry("single char", "a", "minimum length"),
		Entry("two chars", "ab", "two characters"),
		Entry("max length (63 chars)", strings.Repeat("a", 63), "maximum length"),
		Entry("with hyphens", "a-b", "dash in middle"),
		Entry("letters and digits", "a0", "letter followed by digit"),
		Entry("starts with digit", "1abc", "starts with digit"),
		Entry("UUID format", "550e8400-e29b-41d4-a716-446655440000", "UUID format"),
	)

	// TC-U059: network object without ports field is accepted by OpenAPI middleware
	It("accepts network object without ports field (TC-U059)", func() {
		baseURL := startValidationServer()

		body := `{"spec":{"service_type":"container","metadata":{"name":"test"},"image":{"reference":"nginx:latest"},"resources":{"cpu":{"min":1,"max":2},"memory":{"min":"1GB","max":"2GB"}},"network":{}}}`
		resp, err := http.Post(
			baseURL+"/api/v1alpha1/containers",
			"application/json",
			strings.NewReader(body),
		)
		Expect(err).NotTo(HaveOccurred())
		defer func() { _ = resp.Body.Close() }()

		// Network without ports must NOT be rejected by OpenAPI middleware.
		// The handler may still fail (nil repo), but we only check it's not 400.
		Expect(resp.StatusCode).NotTo(Equal(http.StatusBadRequest),
			"network without ports should pass OpenAPI validation")
	})

	// TC-U067: valid request passes OpenAPI middleware and reaches handler
	// with a real 201 response (not a middleware rejection or panic recovery).
	It("passes a valid request through OpenAPI middleware (TC-U067)", func() {
		// Inline server setup with a stub repo so the handler can complete
		// the Create flow. Does NOT modify startValidationServer — TC-U014
		// and TC-U047 deliberately use nil repo for middleware-only testing.
		cfg := &config.Config{
			Server: config.ServerConfig{
				Address:         ":0",
				ShutdownTimeout: 5 * time.Second,
			},
		}
		logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
		ch := container.NewHandler(&stubContainerRepository{}, logger, time.Now(), "0.0.1-test")
		h := oapigen.NewStrictHandlerWithOptions(ch, nil, oapigen.StrictHTTPServerOptions{})
		srv := apiserver.New(cfg, logger, h)

		ln, err := net.Listen("tcp", ":0")
		Expect(err).NotTo(HaveOccurred())
		addr := ln.Addr().String()

		ctx, cancel := context.WithCancel(context.Background())
		DeferCleanup(cancel)

		go func() {
			_ = srv.Run(ctx, ln)
		}()

		Eventually(func() error {
			resp, reqErr := http.Get(fmt.Sprintf("http://%s/api/v1alpha1/containers/health", addr))
			if reqErr != nil {
				return reqErr
			}
			_ = resp.Body.Close()
			return nil
		}).WithTimeout(5 * time.Second).WithPolling(50 * time.Millisecond).Should(Succeed())

		baseURL := fmt.Sprintf("http://%s", addr)

		reqBody := `{"spec":{"service_type":"container","metadata":{"name":"test"},"image":{"reference":"nginx:latest"},"resources":{"cpu":{"min":1,"max":2},"memory":{"min":"1GB","max":"2GB"}}}}`
		resp, err := http.Post(
			baseURL+"/api/v1alpha1/containers",
			"application/json",
			strings.NewReader(reqBody),
		)
		Expect(err).NotTo(HaveOccurred())
		defer func() { _ = resp.Body.Close() }()

		Expect(resp.StatusCode).To(Equal(http.StatusCreated))
		Expect(resp.Header.Get("Content-Type")).NotTo(ContainSubstring("application/problem+json"))

		respBody, err := io.ReadAll(resp.Body)
		Expect(err).NotTo(HaveOccurred())

		var result map[string]any
		Expect(json.Unmarshal(respBody, &result)).To(Succeed())
		Expect(result).To(HaveKey("spec"))
		spec, ok := result["spec"].(map[string]any)
		Expect(ok).To(BeTrue(), "spec should be an object")
		Expect(spec["service_type"]).To(Equal("container"))
		Expect(spec).To(HaveKey("metadata"))
		meta, ok := spec["metadata"].(map[string]any)
		Expect(ok).To(BeTrue(), "metadata should be an object")
		Expect(meta["name"]).To(Equal("test"))
	})
})
