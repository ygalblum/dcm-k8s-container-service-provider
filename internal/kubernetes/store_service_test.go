package kubernetes_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1alpha1 "github.com/dcm-project/k8s-container-service-provider/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("K8s Store", func() {
	Describe("Service Creation", func() {
		// TC-I019: Service created for port with internal visibility
		It("creates ClusterIP Service for port with internal visibility (TC-I019)", func() {
			s, client := newTestStore(defaultConfig())
			c := containerWithVisiblePorts(v1alpha1.Internal, 8080)

			_, err := s.Create(context.Background(), c, "test-id-019")
			Expect(err).NotTo(HaveOccurred())

			svc, svcErr := getCreatedService(client, "default")
			Expect(svcErr).NotTo(HaveOccurred())
			Expect(svc.Spec.Ports).To(HaveLen(1))
			Expect(svc.Spec.Ports[0].Port).To(Equal(int32(8080)))
			Expect(string(svc.Spec.Type)).To(Equal("ClusterIP"))
		})

		// TC-I022: Multiple internal ports in single Service
		It("includes all internal ports in a single Service (TC-I022)", func() {
			s, client := newTestStore(defaultConfig())
			c := containerWithVisiblePorts(v1alpha1.Internal, 8080, 9090)

			_, err := s.Create(context.Background(), c, "test-id-022")
			Expect(err).NotTo(HaveOccurred())

			svcs, err := client.CoreV1().Services("default").List(context.Background(), metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(svcs.Items).To(HaveLen(1))
			Expect(svcs.Items[0].Spec.Ports).To(HaveLen(2))
		})

		// TC-I090: Multi-port Service has named ports for K8s compliance
		It("assigns unique names to each ServicePort (TC-I090)", func() {
			s, client := newTestStore(defaultConfig())
			c := containerWithVisiblePorts(v1alpha1.Internal, 8080, 9090, 3000)

			_, err := s.Create(context.Background(), c, "test-id-090")
			Expect(err).NotTo(HaveOccurred())

			svc, err := getCreatedService(client, "default")
			Expect(err).NotTo(HaveOccurred())
			Expect(svc.Spec.Ports).To(HaveLen(3))
			Expect(svc.Spec.Ports[0].Name).To(Equal("port-8080"))
			Expect(svc.Spec.Ports[1].Name).To(Equal("port-9090"))
			Expect(svc.Spec.Ports[2].Name).To(Equal("port-3000"))
		})

		// TC-I023: Internal-only ports produce ClusterIP Service
		It("uses ClusterIP for internal-only ports (TC-I023)", func() {
			s, client := newTestStore(defaultConfig())
			c := containerWithVisiblePorts(v1alpha1.Internal, 8080)

			_, err := s.Create(context.Background(), c, "test-id-023")
			Expect(err).NotTo(HaveOccurred())

			svc, err := getCreatedService(client, "default")
			Expect(err).NotTo(HaveOccurred())
			Expect(string(svc.Spec.Type)).To(Equal("ClusterIP"))
		})

		// TC-I024: External port uses ExternalServiceType=LoadBalancer
		It("uses ExternalServiceType for external port (TC-I024)", func() {
			cfg := defaultConfig()
			cfg.ExternalServiceType = "LoadBalancer"
			s, client := newTestStore(cfg)
			c := containerWithVisiblePorts(v1alpha1.External, 8080)

			_, err := s.Create(context.Background(), c, "test-id-024")
			Expect(err).NotTo(HaveOccurred())

			svc, err := getCreatedService(client, "default")
			Expect(err).NotTo(HaveOccurred())
			Expect(string(svc.Spec.Type)).To(Equal("LoadBalancer"))
		})

		// TC-I025: All ports visibility=none produces no Service
		It("does not create Service when all ports have visibility=none (TC-I025)", func() {
			s, client := newTestStore(defaultConfig())
			c := containerWithPorts("my-app", 8080) // visibility=none

			_, err := s.Create(context.Background(), c, "test-id-025")
			Expect(err).NotTo(HaveOccurred())

			svcs, err := client.CoreV1().Services("default").List(context.Background(), metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(svcs.Items).To(BeEmpty())
		})

		// TC-I026: No ports produces no Service
		It("does not create Service when no ports defined (TC-I026)", func() {
			s, client := newTestStore(defaultConfig())
			c := minimalContainer("my-app") // no network.ports

			_, err := s.Create(context.Background(), c, "test-id-026")
			Expect(err).NotTo(HaveOccurred())

			svcs, err := client.CoreV1().Services("default").List(context.Background(), metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(svcs.Items).To(BeEmpty())

			// Verify Deployment was still created
			_, deployErr := getCreatedDeployment(client, "default")
			Expect(deployErr).NotTo(HaveOccurred())
		})

		// TC-I027: Service carries DCM labels with internal visibility
		It("carries DCM labels on Service (TC-I027)", func() {
			s, client := newTestStore(defaultConfig())
			c := containerWithVisiblePorts(v1alpha1.Internal, 8080)

			_, err := s.Create(context.Background(), c, "abc-123")
			Expect(err).NotTo(HaveOccurred())

			svc, err := getCreatedService(client, "default")
			Expect(err).NotTo(HaveOccurred())

			Expect(svc.Labels).To(HaveKeyWithValue("dcm.project/managed-by", "dcm"))
			Expect(svc.Labels).To(HaveKeyWithValue("dcm.project/dcm-instance-id", "abc-123"))
			Expect(svc.Labels).To(HaveKeyWithValue("dcm.project/dcm-service-type", "container"))
		})

		// TC-I074: External port uses ExternalServiceType=NodePort
		It("uses NodePort when ExternalServiceType is NodePort (TC-I074)", func() {
			cfg := defaultConfig()
			cfg.ExternalServiceType = "NodePort"
			s, client := newTestStore(cfg)
			c := containerWithVisiblePorts(v1alpha1.External, 8080)

			_, err := s.Create(context.Background(), c, "test-id-074")
			Expect(err).NotTo(HaveOccurred())

			svc, err := getCreatedService(client, "default")
			Expect(err).NotTo(HaveOccurred())
			Expect(string(svc.Spec.Type)).To(Equal("NodePort"))
		})

		// TC-I091: Mixed visibility — only non-none ports in Service
		It("includes only non-none ports in Service (TC-I091)", func() {
			s, client := newTestStore(defaultConfig())
			c := minimalContainer("my-app")
			ports := []v1alpha1.ContainerPort{
				{ContainerPort: 8080, Visibility: v1alpha1.Internal},
				{ContainerPort: 9090, Visibility: v1alpha1.None},
			}
			c.Network = &v1alpha1.ContainerNetwork{Ports: &ports}

			_, err := s.Create(context.Background(), c, "test-id-091")
			Expect(err).NotTo(HaveOccurred())

			svc, err := getCreatedService(client, "default")
			Expect(err).NotTo(HaveOccurred())
			Expect(svc.Spec.Ports).To(HaveLen(1))
			Expect(svc.Spec.Ports[0].Port).To(Equal(int32(8080)))
		})

		// TC-I092: Mixed internal+external — external promotes Service type
		It("promotes Service type when external port present (TC-I092)", func() {
			cfg := defaultConfig()
			cfg.ExternalServiceType = "LoadBalancer"
			s, client := newTestStore(cfg)
			c := minimalContainer("my-app")
			ports := []v1alpha1.ContainerPort{
				{ContainerPort: 8080, Visibility: v1alpha1.Internal},
				{ContainerPort: 9090, Visibility: v1alpha1.External},
			}
			c.Network = &v1alpha1.ContainerNetwork{Ports: &ports}

			_, err := s.Create(context.Background(), c, "test-id-092")
			Expect(err).NotTo(HaveOccurred())

			svc, err := getCreatedService(client, "default")
			Expect(err).NotTo(HaveOccurred())
			Expect(string(svc.Spec.Type)).To(Equal("LoadBalancer"))
			Expect(svc.Spec.Ports).To(HaveLen(2))
		})

		// TC-I093: GET infers internal when Service is ClusterIP
		It("infers internal visibility when Service is ClusterIP (TC-I093)", func() {
			s, client := newTestStore(defaultConfig())
			c := containerWithVisiblePorts(v1alpha1.Internal, 8080)

			created, err := s.Create(context.Background(), c, "test-id-093")
			Expect(err).NotTo(HaveOccurred())

			// Create a matching Pod so Get can succeed
			err = createFakePod(client, "my-app-pod", "test-id-093", "Running", "10.0.0.1")
			Expect(err).NotTo(HaveOccurred())

			got, err := s.Get(context.Background(), *created.Id)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Spec.Network).NotTo(BeNil())
			Expect(got.Spec.Network.Ports).NotTo(BeNil())
			ports := *got.Spec.Network.Ports
			Expect(ports).To(HaveLen(1))
			Expect(ports[0].Visibility).To(Equal(v1alpha1.Internal))
		})

		// TC-I094: GET infers external when Service is LoadBalancer
		It("infers external visibility when Service is LoadBalancer (TC-I094)", func() {
			cfg := defaultConfig()
			cfg.ExternalServiceType = "LoadBalancer"
			s, client := newTestStore(cfg)
			c := containerWithVisiblePorts(v1alpha1.External, 8080)

			created, err := s.Create(context.Background(), c, "test-id-094")
			Expect(err).NotTo(HaveOccurred())

			// Create a matching Pod so Get can succeed
			err = createFakePod(client, "my-app-pod", "test-id-094", "Running", "10.0.0.1")
			Expect(err).NotTo(HaveOccurred())

			got, err := s.Get(context.Background(), *created.Id)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Spec.Network).NotTo(BeNil())
			Expect(got.Spec.Network.Ports).NotTo(BeNil())
			ports := *got.Spec.Network.Ports
			Expect(ports).To(HaveLen(1))
			Expect(ports[0].Visibility).To(Equal(v1alpha1.External))
		})

		// TC-I095: GET infers none when no Service exists
		It("infers none visibility when no Service exists (TC-I095)", func() {
			s, client := newTestStore(defaultConfig())
			c := containerWithPorts("my-app", 8080) // visibility=none, no service created

			created, err := s.Create(context.Background(), c, "test-id-095")
			Expect(err).NotTo(HaveOccurred())

			// Create a matching Pod so Get can succeed
			err = createFakePod(client, "my-app-pod", "test-id-095", "Running", "10.0.0.1")
			Expect(err).NotTo(HaveOccurred())

			got, err := s.Get(context.Background(), *created.Id)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Spec.Network).NotTo(BeNil())
			Expect(got.Spec.Network.Ports).NotTo(BeNil())
			ports := *got.Spec.Network.Ports
			Expect(ports).To(HaveLen(1))
			Expect(ports[0].Visibility).To(Equal(v1alpha1.None))
		})

		// TC-I111: Network without ports creates no Service
		It("creates Deployment but no Service when network has no ports (TC-I111)", func() {
			s, client := newTestStore(defaultConfig())
			c := minimalContainer("my-app")
			c.Network = &v1alpha1.ContainerNetwork{} // network present, ports absent

			_, err := s.Create(context.Background(), c, "test-id-111")
			Expect(err).NotTo(HaveOccurred())

			// Deployment must be created
			_, deployErr := getCreatedDeployment(client, "default")
			Expect(deployErr).NotTo(HaveOccurred())

			// No Service should exist
			svcs, svcErr := client.CoreV1().Services("default").List(context.Background(), metav1.ListOptions{})
			Expect(svcErr).NotTo(HaveOccurred())
			Expect(svcs.Items).To(BeEmpty())
		})

		// TC-I112: Provider hints do not affect K8s resources
		It("creates identical resources with or without provider_hints (TC-I112)", func() {
			s, client := newTestStore(defaultConfig())
			c := containerWithVisiblePorts(v1alpha1.Internal, 8080)
			hints := map[string]interface{}{"gpu": true, "zone": "us-east-1"}
			c.ProviderHints = &hints

			_, err := s.Create(context.Background(), c, "test-id-112")
			Expect(err).NotTo(HaveOccurred())

			// Deployment should be created normally
			deploy, deployErr := getCreatedDeployment(client, "default")
			Expect(deployErr).NotTo(HaveOccurred())
			Expect(deploy.Spec.Template.Spec.Containers).To(HaveLen(1))
			Expect(deploy.Spec.Template.Spec.Containers[0].Image).To(Equal("nginx:latest"))

			// Service should be created normally
			svc, svcErr := getCreatedService(client, "default")
			Expect(svcErr).NotTo(HaveOccurred())
			Expect(svc.Spec.Ports).To(HaveLen(1))
		})
	})
})
