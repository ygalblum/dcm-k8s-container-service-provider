package kubernetes_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	k8sstore "github.com/dcm-project/k8s-container-service-provider/internal/kubernetes"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

var _ = Describe("K8s Store", func() {
	Describe("Health Check", func() {
		// TC-I116: CheckHealth succeeds with reachable fake client
		It("succeeds when the K8s API server is reachable (TC-I116)", func() {
			s, _ := newTestStore(defaultConfig())

			err := s.CheckHealth(context.Background())
			Expect(err).NotTo(HaveOccurred())
		})

		// TC-I117: CheckHealth returns error when Discovery fails
		It("returns error when Discovery endpoint fails (TC-I117)", func() {
			cfg := defaultConfig()
			client := fake.NewClientset()
			logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
			s := k8sstore.NewK8sContainerStore(client, cfg, logger)

			client.PrependReactor("get", "version", func(_ k8stesting.Action) (bool, runtime.Object, error) {
				return true, nil, fmt.Errorf("simulated discovery failure")
			})

			err := s.CheckHealth(context.Background())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("simulated discovery failure"))
		})
	})
})
