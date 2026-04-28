package kubernetes_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1alpha1 "github.com/dcm-project/k8s-container-service-provider/api/v1alpha1"
	k8sstore "github.com/dcm-project/k8s-container-service-provider/internal/kubernetes"
	"github.com/dcm-project/k8s-container-service-provider/internal/store"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

var _ = Describe("K8s Store", func() {
	Describe("Conflict & Namespace", func() {
		// TC-I028: GenerateName allows same metadata name with different IDs
		It("allows same metadata name with different instance IDs (TC-I028)", func() {
			s, client := newTestStore(defaultConfig())

			// Pre-create a Deployment with name "web-app"
			err := createFakeDeployment(client, "web-app", "original-id")
			Expect(err).NotTo(HaveOccurred())

			// Create with the same metadata name but different ID — succeeds
			// because GenerateName produces a unique Deployment name.
			c := minimalContainer("web-app")
			_, err = s.Create(context.Background(), c, "different-id")
			Expect(err).NotTo(HaveOccurred())

			// Verify both Deployments exist
			deployList, listErr := client.AppsV1().Deployments("default").List(context.Background(), metav1.ListOptions{})
			Expect(listErr).NotTo(HaveOccurred())
			Expect(deployList.Items).To(HaveLen(2))
		})

		// TC-I029: All resources created in the configured namespace
		It("creates all resources in the configured namespace (TC-I029)", func() {
			cfg := k8sstore.K8sConfig{
				Namespace:           "production",
				ExternalServiceType: "LoadBalancer",
			}
			s, client := newTestStore(cfg)
			c := containerWithVisiblePorts(v1alpha1.Internal, 8080)

			_, err := s.Create(context.Background(), c, "test-id-029")
			Expect(err).NotTo(HaveOccurred())

			// Verify Deployment is in "production" namespace
			deploy, err := getCreatedDeployment(client, "production")
			Expect(err).NotTo(HaveOccurred())
			Expect(deploy.Namespace).To(Equal("production"))

			// Verify Service is in "production" namespace
			svc, err := getCreatedService(client, "production")
			Expect(err).NotTo(HaveOccurred())
			Expect(svc.Namespace).To(Equal("production"))
		})

		// TC-I088: Service creation failure triggers Deployment rollback
		It("rolls back Deployment when Service creation fails (TC-I088)", func() {
			cfg := defaultConfig()
			client := fake.NewClientset()
			logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
			s := k8sstore.NewK8sContainerStore(client, cfg, logger)

			// Inject a Service creation error
			client.PrependReactor("create", "services", func(_ k8stesting.Action) (bool, runtime.Object, error) {
				return true, nil, fmt.Errorf("simulated service creation failure")
			})

			c := containerWithVisiblePorts(v1alpha1.Internal, 8080)
			_, err := s.Create(context.Background(), c, "test-id-088")

			// Error should propagate
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("simulated service creation failure"))

			// Error must NOT be a typed store error (it's a raw infra error)
			var notFoundErr *store.NotFoundError
			var conflictErr *store.ConflictError
			Expect(errors.As(err, &notFoundErr)).To(BeFalse())
			Expect(errors.As(err, &conflictErr)).To(BeFalse())

			// Verify rollback: Deployment should have been deleted
			deployList, listErr := client.AppsV1().Deployments("default").List(
				context.Background(), metav1.ListOptions{},
			)
			Expect(listErr).NotTo(HaveOccurred())
			Expect(deployList.Items).To(BeEmpty(), "Deployment should be rolled back after Service creation failure")
		})

		// TC-I081: Unexpected K8s API error produces internal store error
		It("produces internal store error on unexpected K8s API error (TC-I081)", func() {
			cfg := defaultConfig()
			client := fake.NewClientset()
			logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
			s := k8sstore.NewK8sContainerStore(client, cfg, logger)

			// Inject a K8s API error on Deployment creation
			client.PrependReactor("create", "deployments", func(_ k8stesting.Action) (bool, runtime.Object, error) {
				return true, nil, apierrors.NewInternalError(fmt.Errorf("etcd cluster unavailable"))
			})

			c := minimalContainer("my-app")
			_, err := s.Create(context.Background(), c, "test-id-081")
			Expect(err).To(HaveOccurred())

			// Error must NOT be a typed store error
			var notFoundErr *store.NotFoundError
			var conflictErr *store.ConflictError
			Expect(errors.As(err, &notFoundErr)).To(BeFalse(), "error should not be NotFoundError")
			Expect(errors.As(err, &conflictErr)).To(BeFalse(), "error should not be ConflictError")

			// Error should contain K8s API error detail (reactor was triggered)
			Expect(err.Error()).To(ContainSubstring("etcd cluster unavailable"))
		})
	})
})
