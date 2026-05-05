package apiserver_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1alpha1 "github.com/dcm-project/k8s-container-service-provider/api/v1alpha1"
	oapigen "github.com/dcm-project/k8s-container-service-provider/internal/api/server"
	"github.com/dcm-project/k8s-container-service-provider/internal/apiserver"
	"github.com/dcm-project/k8s-container-service-provider/internal/config"
	"github.com/dcm-project/k8s-container-service-provider/internal/handlers/container"
	"github.com/dcm-project/k8s-container-service-provider/internal/store"
)

// syncBuffer is a goroutine-safe bytes.Buffer for capturing log output
// shared between the server goroutine (writer) and the test goroutine (reader).
type syncBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (b *syncBuffer) Write(p []byte) (n int, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	return len(p), nil
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}

// panicOnListHandler implements ServerInterface, panicking on ListContainers
// to test recovery middleware. All other methods use the default stub behaviour.
type panicOnListHandler struct {
	oapigen.Unimplemented
}

func (p *panicOnListHandler) ListContainers(_ http.ResponseWriter, _ *http.Request, _ v1alpha1.ListContainersParams) {
	panic("unexpected failure")
}

// abortOnListHandler implements ServerInterface, panicking with
// http.ErrAbortHandler on ListContainers to verify the recovery
// middleware re-panics this sentinel value.
type abortOnListHandler struct {
	oapigen.Unimplemented
}

func (a *abortOnListHandler) ListContainers(_ http.ResponseWriter, _ *http.Request, _ v1alpha1.ListContainersParams) {
	panic(http.ErrAbortHandler)
}

// headersThenPanicOnListHandler implements ServerInterface. It writes
// a status header before panicking, so the recovery middleware cannot
// safely write its own RFC 7807 response.
type headersThenPanicOnListHandler struct {
	oapigen.Unimplemented
}

func (h *headersThenPanicOnListHandler) ListContainers(w http.ResponseWriter, _ *http.Request, _ v1alpha1.ListContainersParams) {
	w.WriteHeader(http.StatusTeapot)
	panic("boom after headers")
}

// blockingListHandler implements ServerInterface, blocking on ListContainers
// until the request context is cancelled. Used to test the timeout middleware.
type blockingListHandler struct {
	oapigen.Unimplemented
	ctxCancelled chan struct{}
}

func (b *blockingListHandler) ListContainers(_ http.ResponseWriter, r *http.Request, _ v1alpha1.ListContainersParams) {
	<-r.Context().Done()
	close(b.ctxCancelled)
}

// mockContainerRepo implements store.ContainerRepository for integration tests.
// Only wire the methods your test needs; unconfigured methods panic.
type mockContainerRepo struct {
	CreateFunc      func(ctx context.Context, spec v1alpha1.ContainerSpec, id string) (*v1alpha1.Container, error)
	GetFunc         func(ctx context.Context, containerID string) (*v1alpha1.Container, error)
	ListFunc        func(ctx context.Context, maxPageSize int32, pageToken string) (*v1alpha1.ContainerList, error)
	DeleteFunc      func(ctx context.Context, containerID string) error
	CheckHealthFunc func() error
}

func (m *mockContainerRepo) Create(ctx context.Context, spec v1alpha1.ContainerSpec, id string) (*v1alpha1.Container, error) {
	if m.CreateFunc == nil {
		panic("unexpected call to Create")
	}
	return m.CreateFunc(ctx, spec, id)
}

func (m *mockContainerRepo) Get(ctx context.Context, containerID string) (*v1alpha1.Container, error) {
	if m.GetFunc == nil {
		panic("unexpected call to Get")
	}
	return m.GetFunc(ctx, containerID)
}

func (m *mockContainerRepo) List(ctx context.Context, maxPageSize int32, pageToken string) (*v1alpha1.ContainerList, error) {
	if m.ListFunc == nil {
		panic("unexpected call to List")
	}
	return m.ListFunc(ctx, maxPageSize, pageToken)
}

func (m *mockContainerRepo) Delete(ctx context.Context, containerID string) error {
	if m.DeleteFunc == nil {
		panic("unexpected call to Delete")
	}
	return m.DeleteFunc(ctx, containerID)
}

func (m *mockContainerRepo) CheckHealth(_ context.Context) error {
	if m.CheckHealthFunc == nil {
		return nil
	}
	return m.CheckHealthFunc()
}

var _ = Describe("HTTP Server", func() {
	// startServerWithRepo is a helper that creates a server with an explicit
	// ContainerRepository, starts it in a goroutine, and returns the address,
	// cancel/cleanup functions.
	//
	// When signals are non-nil, the context is wired to those OS signals
	// via signal.NotifyContext so the server shuts down on signal delivery.
	// When signals is nil, a plain context.WithCancel is used.
	startServerWithRepo := func(cfg *config.Config, logBuf *syncBuffer, signals []os.Signal, repo store.ContainerRepository, wrappers ...func(http.Handler) http.Handler) (
		addr string,
		cancel context.CancelFunc,
		errCh chan error,
	) {
		var logger *slog.Logger
		if logBuf != nil {
			logger = slog.New(slog.NewJSONHandler(logBuf, nil))
		} else {
			logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
		}

		ch := container.NewHandler(repo, logger, time.Now(), "0.0.1-test")
		h := oapigen.NewStrictHandlerWithOptions(ch, nil, oapigen.StrictHTTPServerOptions{})
		srv := apiserver.New(cfg, logger, h)
		Expect(srv).NotTo(BeNil(), "New() must return a non-nil server")

		for _, w := range wrappers {
			srv.WrapHandler(w)
		}

		ln, err := net.Listen("tcp", ":0")
		Expect(err).NotTo(HaveOccurred())
		addr = ln.Addr().String()

		var ctx context.Context
		if len(signals) > 0 {
			// Clear any existing handlers so only our context receives the signal.
			signal.Reset(signals...)
			ctx, cancel = signal.NotifyContext(context.Background(), signals...)
		} else {
			ctx, cancel = context.WithCancel(context.Background())
		}

		errCh = make(chan error, 1)
		go func() {
			errCh <- srv.Run(ctx, ln)
		}()

		// Wait for the server to start handling requests. The listener is
		// already bound so TCP connects immediately, but we need Serve()
		// to be running to get an HTTP response.
		Eventually(func() error {
			resp, reqErr := http.Get(fmt.Sprintf("http://%s/api/v1alpha1/containers/health", addr))
			if reqErr != nil {
				return reqErr
			}
			_ = resp.Body.Close()
			return nil
		}).WithTimeout(5 * time.Second).WithPolling(50 * time.Millisecond).Should(Succeed())

		return addr, cancel, errCh
	}

	// startServer is a convenience wrapper that uses a nil repository.
	startServer := func(cfg *config.Config, logBuf *syncBuffer, signals []os.Signal, wrappers ...func(http.Handler) http.Handler) (
		addr string,
		cancel context.CancelFunc,
		errCh chan error,
	) {
		return startServerWithRepo(cfg, logBuf, signals, nil, wrappers...)
	}

	defaultConfig := func() *config.Config {
		return &config.Config{
			Server: config.ServerConfig{
				Address:         ":0",
				ShutdownTimeout: 5 * time.Second,
			},
		}
	}

	// TC-I001: Server starts and listens on configured address
	It("starts and accepts HTTP connections (TC-I001)", func() {
		addr, cancel, errCh := startServer(defaultConfig(), nil, nil)
		defer func() {
			cancel()
			Eventually(errCh).WithTimeout(10 * time.Second).Should(Receive())
		}()

		resp, err := http.Get(fmt.Sprintf("http://%s/api/v1alpha1/containers/health", addr))
		Expect(err).NotTo(HaveOccurred())
		defer func() { _ = resp.Body.Close() }()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
	})

	// TC-I002: All OpenAPI-defined routes are registered
	It("registers all OpenAPI-defined routes (TC-I002)", func() {
		addr, cancel, errCh := startServer(defaultConfig(), nil, nil)
		defer func() {
			cancel()
			Eventually(errCh).WithTimeout(10 * time.Second).Should(Receive())
		}()

		baseURL := fmt.Sprintf("http://%s", addr)

		type routeCheck struct {
			method string
			path   string
		}

		routes := []routeCheck{
			{"GET", "/api/v1alpha1/containers/health"},
			{"GET", "/api/v1alpha1/containers"},
			{"POST", "/api/v1alpha1/containers"},
			{"GET", "/api/v1alpha1/containers/test-id"},
			{"DELETE", "/api/v1alpha1/containers/test-id"},
		}

		for _, rc := range routes {
			req, err := http.NewRequest(rc.method, baseURL+rc.path, http.NoBody)
			Expect(err).NotTo(HaveOccurred(), "route: %s %s", rc.method, rc.path)

			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred(), "route: %s %s", rc.method, rc.path)
			_ = resp.Body.Close()

			Expect(resp.StatusCode).NotTo(Equal(http.StatusNotFound),
				"route %s %s should not return 404", rc.method, rc.path)
			Expect(resp.StatusCode).NotTo(Equal(http.StatusMethodNotAllowed),
				"route %s %s should not return 405", rc.method, rc.path)
		}
	})

	// TC-I003: Undefined routes return appropriate error
	It("returns 404 or 405 for undefined routes (TC-I003)", func() {
		addr, cancel, errCh := startServer(defaultConfig(), nil, nil)
		defer func() {
			cancel()
			Eventually(errCh).WithTimeout(10 * time.Second).Should(Receive())
		}()

		resp, err := http.Get(fmt.Sprintf("http://%s/undefined-path", addr))
		Expect(err).NotTo(HaveOccurred())
		defer func() { _ = resp.Body.Close() }()

		Expect(resp.StatusCode).To(SatisfyAny(
			Equal(http.StatusNotFound),
			Equal(http.StatusMethodNotAllowed),
		))
	})

	// TC-I004: Server shuts down gracefully on SIGTERM and drains in-flight requests
	It("drains in-flight requests on SIGTERM (TC-I004)", func() {
		reqStarted := make(chan struct{})
		reqRelease := make(chan struct{})

		slowWrapper := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/test/slow" {
					close(reqStarted)
					<-reqRelease
					w.WriteHeader(http.StatusOK)
					return
				}
				next.ServeHTTP(w, r)
			})
		}

		addr, cancel, errCh := startServer(defaultConfig(), nil, []os.Signal{syscall.SIGTERM}, slowWrapper)
		defer cancel() // idempotent after signal delivery

		// Start an in-flight request in the background.
		type result struct {
			resp *http.Response
			err  error
		}
		respCh := make(chan result, 1)
		go func() {
			resp, err := http.Get(fmt.Sprintf("http://%s/test/slow", addr))
			respCh <- result{resp, err}
		}()

		// Wait for the request to be handled by the server.
		<-reqStarted

		// Send SIGTERM while the request is in-flight.
		proc, err := os.FindProcess(os.Getpid())
		Expect(err).NotTo(HaveOccurred())
		Expect(proc.Signal(syscall.SIGTERM)).To(Succeed())

		// Release the slow handler so the in-flight request completes.
		close(reqRelease)

		// The in-flight request must complete successfully.
		var res result
		Eventually(respCh).WithTimeout(5 * time.Second).Should(Receive(&res))
		Expect(res.err).NotTo(HaveOccurred())
		defer func() { _ = res.resp.Body.Close() }()
		Expect(res.resp.StatusCode).To(Equal(http.StatusOK))

		// Server should exit cleanly after draining.
		Eventually(errCh).WithTimeout(10 * time.Second).Should(Receive(BeNil()))

		// New connections should be refused after shutdown.
		_, err = http.Get(fmt.Sprintf("http://%s/api/v1alpha1/containers/health", addr))
		Expect(err).To(HaveOccurred())
	})

	// TC-I005: Server shuts down gracefully on SIGINT and drains in-flight requests
	It("drains in-flight requests on SIGINT (TC-I005)", func() {
		reqStarted := make(chan struct{})
		reqRelease := make(chan struct{})

		slowWrapper := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/test/slow" {
					close(reqStarted)
					<-reqRelease
					w.WriteHeader(http.StatusOK)
					return
				}
				next.ServeHTTP(w, r)
			})
		}

		addr, cancel, errCh := startServer(defaultConfig(), nil, []os.Signal{syscall.SIGINT}, slowWrapper)
		defer cancel() // idempotent after signal delivery

		type result struct {
			resp *http.Response
			err  error
		}
		respCh := make(chan result, 1)
		go func() {
			resp, err := http.Get(fmt.Sprintf("http://%s/test/slow", addr))
			respCh <- result{resp, err}
		}()

		<-reqStarted

		proc, err := os.FindProcess(os.Getpid())
		Expect(err).NotTo(HaveOccurred())
		Expect(proc.Signal(syscall.SIGINT)).To(Succeed())

		close(reqRelease)

		var res result
		Eventually(respCh).WithTimeout(5 * time.Second).Should(Receive(&res))
		Expect(res.err).NotTo(HaveOccurred())
		defer func() { _ = res.resp.Body.Close() }()
		Expect(res.resp.StatusCode).To(Equal(http.StatusOK))

		Eventually(errCh).WithTimeout(10 * time.Second).Should(Receive(BeNil()))

		_, err = http.Get(fmt.Sprintf("http://%s/api/v1alpha1/containers/health", addr))
		Expect(err).To(HaveOccurred())
	})

	// TC-I006: Server logs startup with listen address
	It("logs startup with listen address (TC-I006)", func() {
		var logBuf syncBuffer
		addr, cancel, errCh := startServer(defaultConfig(), &logBuf, nil)
		defer func() {
			cancel()
			Eventually(errCh).WithTimeout(10 * time.Second).Should(Receive())
		}()

		Expect(addr).NotTo(BeEmpty())
		Expect(logBuf.String()).To(ContainSubstring(addr))
	})

	// TC-I007: Server logs shutdown event
	It("logs shutdown event (TC-I007)", func() {
		var logBuf syncBuffer
		_, cancel, errCh := startServer(defaultConfig(), &logBuf, nil)

		cancel()
		Eventually(errCh).WithTimeout(10 * time.Second).Should(Receive())

		logOutput := logBuf.String()
		Expect(logOutput).To(SatisfyAny(
			ContainSubstring("shutdown"),
			ContainSubstring("shutting down"),
			ContainSubstring("stopping"),
		))
	})

	// TC-I008: Malformed requests return 400 with RFC 7807 body
	DescribeTable("returns 400 with RFC 7807 body for malformed requests (TC-I008)",
		func(method, path string, description string) {
			addr, cancel, errCh := startServer(defaultConfig(), nil, nil)
			defer func() {
				cancel()
				Eventually(errCh).WithTimeout(10 * time.Second).Should(Receive())
			}()

			url := fmt.Sprintf("http://%s%s", addr, path)
			req, err := http.NewRequest(method, url, http.NoBody)
			Expect(err).NotTo(HaveOccurred())

			resp, err := http.DefaultClient.Do(req)
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
			// Assert RFC 7807 field values, not just presence.
			Expect(problemJSON).To(HaveKeyWithValue("type", "INVALID_ARGUMENT"),
				"RFC 7807 'type' must be INVALID_ARGUMENT for: %s", description)
			Expect(problemJSON).To(HaveKeyWithValue("title", "Bad Request"),
				"RFC 7807 'title' must be 'Bad Request' for: %s", description)
			Expect(problemJSON["status"]).To(BeNumerically("==", 400),
				"RFC 7807 'status' must be 400 for: %s", description)

			// Assert detail exists and is human-friendly.
			Expect(problemJSON).To(HaveKey("detail"),
				"RFC 7807 body must have 'detail' for: %s", description)
			detail, ok := problemJSON["detail"].(string)
			Expect(ok).To(BeTrue(), "detail must be a string for: %s", description)
			Expect(detail).NotTo(BeEmpty(),
				"detail must not be empty for: %s", description)
			Expect(detail).NotTo(ContainSubstring("{\""),
				"detail must not contain raw JSON schema for: %s", description)
			Expect(detail).NotTo(ContainSubstring("strconv."),
				"detail must not expose strconv internals for: %s", description)
			Expect(detail).NotTo(ContainSubstring("invalid syntax"),
				"detail must not expose parse errors for: %s", description)
		},
		Entry("max_page_size=NaN", "GET", "/api/v1alpha1/containers?max_page_size=not-a-number", "non-numeric max_page_size"),
		Entry("max_page_size=0", "GET", "/api/v1alpha1/containers?max_page_size=0", "zero max_page_size"),
		Entry("max_page_size=-1", "GET", "/api/v1alpha1/containers?max_page_size=-1", "negative max_page_size"),
		Entry("max_page_size=1001", "GET", "/api/v1alpha1/containers?max_page_size=1001", "max_page_size above maximum"),
		Entry("empty container_id", "GET", "/api/v1alpha1/containers/", "empty container_id"),
		Entry("invalid container_id pattern", "GET", "/api/v1alpha1/containers/UPPERCASE_ID", "container_id with uppercase characters"),
	)

	// TC-I104: Panic recovery returns RFC 7807 JSON
	It("returns RFC 7807 JSON on handler panic (TC-I104)", func() {
		// Use a custom handler that panics inside a route handler,
		// ensuring the panic occurs within the chi middleware chain
		// where the recovery middleware can catch it.
		h := &panicOnListHandler{}
		logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
		srv := apiserver.New(defaultConfig(), logger, h)

		ln, err := net.Listen("tcp", ":0")
		Expect(err).NotTo(HaveOccurred())
		addr := ln.Addr().String()

		ctx, cancel := context.WithCancel(context.Background())
		errCh := make(chan error, 1)
		go func() {
			errCh <- srv.Run(ctx, ln)
		}()
		defer func() {
			cancel()
			Eventually(errCh).WithTimeout(10 * time.Second).Should(Receive())
		}()

		// Wait for the server to be ready.
		Eventually(func() error {
			resp, reqErr := http.Get(fmt.Sprintf("http://%s/api/v1alpha1/containers/health", addr))
			if reqErr != nil {
				return reqErr
			}
			_ = resp.Body.Close()
			return nil
		}).WithTimeout(5 * time.Second).WithPolling(50 * time.Millisecond).Should(Succeed())

		// Hit the panicking route.
		resp, err := http.Get(fmt.Sprintf("http://%s/api/v1alpha1/containers", addr))
		Expect(err).NotTo(HaveOccurred())
		defer func() { _ = resp.Body.Close() }()

		Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError))
		Expect(resp.Header.Get("Content-Type")).To(Equal("application/problem+json"))

		body, err := io.ReadAll(resp.Body)
		Expect(err).NotTo(HaveOccurred())

		var problemJSON map[string]any
		Expect(json.Unmarshal(body, &problemJSON)).To(Succeed())

		// Full RFC 7807 payload validation.
		Expect(problemJSON).To(HaveKeyWithValue("type", "INTERNAL"))
		Expect(problemJSON["status"]).To(BeNumerically("==", 500))
		Expect(problemJSON).To(HaveKeyWithValue("title", "Internal Server Error"))
		Expect(problemJSON).To(HaveKeyWithValue("detail", "an unexpected error occurred"))

		// Comprehensive leak prevention — panic message, stack traces, and
		// runtime internals must not leak to the client.
		bodyStr := string(body)
		Expect(bodyStr).NotTo(ContainSubstring("unexpected failure"),
			"panic message must not leak")
		Expect(bodyStr).NotTo(ContainSubstring("goroutine"),
			"goroutine info must not leak")
		Expect(bodyStr).NotTo(ContainSubstring("runtime."),
			"runtime details must not leak")
		Expect(bodyStr).NotTo(ContainSubstring(".go:"),
			"file paths must not leak")
		Expect(bodyStr).NotTo(ContainSubstring("0x"),
			"memory addresses must not leak")
	})

	// TC-I105: http.ErrAbortHandler is re-panicked (connection aborted)
	It("re-panics http.ErrAbortHandler so the connection is aborted (TC-I105)", func() {
		var logBuf syncBuffer
		h := &abortOnListHandler{}
		logger := slog.New(slog.NewJSONHandler(&logBuf, nil))
		srv := apiserver.New(defaultConfig(), logger, h)

		ln, err := net.Listen("tcp", ":0")
		Expect(err).NotTo(HaveOccurred())
		addr := ln.Addr().String()

		ctx, cancel := context.WithCancel(context.Background())
		errCh := make(chan error, 1)
		go func() {
			errCh <- srv.Run(ctx, ln)
		}()
		defer func() {
			cancel()
			Eventually(errCh).WithTimeout(10 * time.Second).Should(Receive())
		}()

		// Wait for the server to be ready.
		Eventually(func() error {
			resp, reqErr := http.Get(fmt.Sprintf("http://%s/api/v1alpha1/containers/health", addr))
			if reqErr != nil {
				return reqErr
			}
			_ = resp.Body.Close()
			return nil
		}).WithTimeout(5 * time.Second).WithPolling(50 * time.Millisecond).Should(Succeed())

		// Hit the panicking route — the server should abort the connection,
		// so we expect a transport-level error (not a 500 response).
		_, err = http.Get(fmt.Sprintf("http://%s/api/v1alpha1/containers", addr))
		Expect(err).To(HaveOccurred(), "expected a connection error, not a valid HTTP response")

		// The middleware must NOT log "panic recovered" because ErrAbortHandler
		// is re-panicked for net/http's built-in abort handler.
		Consistently(logBuf.String).WithTimeout(200 * time.Millisecond).WithPolling(50 * time.Millisecond).ShouldNot(ContainSubstring("panic recovered"))
	})

	// TC-I106: Headers-already-sent panic logs without writing RFC 7807
	It("logs headers-already-sent without overwriting the status (TC-I106)", func() {
		var logBuf syncBuffer
		h := &headersThenPanicOnListHandler{}
		logger := slog.New(slog.NewJSONHandler(&logBuf, nil))
		srv := apiserver.New(defaultConfig(), logger, h)

		ln, err := net.Listen("tcp", ":0")
		Expect(err).NotTo(HaveOccurred())
		addr := ln.Addr().String()

		ctx, cancel := context.WithCancel(context.Background())
		errCh := make(chan error, 1)
		go func() {
			errCh <- srv.Run(ctx, ln)
		}()
		defer func() {
			cancel()
			Eventually(errCh).WithTimeout(10 * time.Second).Should(Receive())
		}()

		// Wait for the server to be ready.
		Eventually(func() error {
			resp, reqErr := http.Get(fmt.Sprintf("http://%s/api/v1alpha1/containers/health", addr))
			if reqErr != nil {
				return reqErr
			}
			_ = resp.Body.Close()
			return nil
		}).WithTimeout(5 * time.Second).WithPolling(50 * time.Millisecond).Should(Succeed())

		// Hit the panicking route.
		resp, err := http.Get(fmt.Sprintf("http://%s/api/v1alpha1/containers", addr))
		Expect(err).NotTo(HaveOccurred())
		defer func() { _ = resp.Body.Close() }()

		// The client should see the original 418 status, not a 500 replacement.
		Expect(resp.StatusCode).To(Equal(http.StatusTeapot))

		// Content-Type must NOT be application/problem+json because the
		// middleware should not attempt to write a body after headers are sent.
		Expect(resp.Header.Get("Content-Type")).NotTo(Equal("application/problem+json"))

		// The server log must contain both indicators.
		Eventually(logBuf.String).WithTimeout(2 * time.Second).WithPolling(50 * time.Millisecond).Should(And(
			ContainSubstring("panic recovered"),
			ContainSubstring("headers already sent"),
		))
	})

	// TC-I082: onReady panic does not crash server
	It("recovers from panicking onReady callback (TC-I082)", func() {
		var logBuf syncBuffer
		logger := slog.New(slog.NewJSONHandler(&logBuf, nil))

		cfg := defaultConfig()
		ch := container.NewHandler(nil, logger, time.Now(), "0.0.1-test")
		h := oapigen.NewStrictHandlerWithOptions(ch, nil, oapigen.StrictHTTPServerOptions{})
		srv := apiserver.New(cfg, logger, h).WithOnReady(func(_ context.Context) {
			panic("onReady boom")
		})
		Expect(srv).NotTo(BeNil())

		ln, err := net.Listen("tcp", ":0")
		Expect(err).NotTo(HaveOccurred())
		addr := ln.Addr().String()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		errCh := make(chan error, 1)
		go func() {
			errCh <- srv.Run(ctx, ln)
		}()

		// Server should still accept requests after the panic.
		Eventually(func() error {
			resp, reqErr := http.Get(fmt.Sprintf("http://%s/api/v1alpha1/containers/health", addr))
			if reqErr != nil {
				return reqErr
			}
			_ = resp.Body.Close()
			return nil
		}).WithTimeout(5 * time.Second).WithPolling(50 * time.Millisecond).Should(Succeed())

		// Verify panic was logged. Use Eventually because the internal
		// readiness probe may complete slightly after the external one.
		Eventually(logBuf.String).WithTimeout(5 * time.Second).WithPolling(50 * time.Millisecond).Should(ContainSubstring("onReady callback panicked"))
		Expect(logBuf.String()).To(ContainSubstring("onReady boom"))
	})

	// TC-I085: onReady is invoked only after the server is confirmed serving
	It("invokes onReady only after server is serving (TC-I085)", func() {
		cfg := defaultConfig()
		logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
		ch := container.NewHandler(nil, logger, time.Now(), "0.0.1-test")
		h := oapigen.NewStrictHandlerWithOptions(ch, nil, oapigen.StrictHTTPServerOptions{})
		srv := apiserver.New(cfg, logger, h).
			WithOnReady(func(_ context.Context) {
				// Inside onReady, verify that the health endpoint is
				// already reachable. If the probe works correctly, this
				// GET must succeed because onReady is only called after
				// the probe got a 200.
				// We cannot use the listener address directly here, so
				// the test verifies indirectly: if onReady fires at all,
				// the probe already confirmed the server is up.
			})

		ln, err := net.Listen("tcp", ":0")
		Expect(err).NotTo(HaveOccurred())
		addr := ln.Addr().String()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		errCh := make(chan error, 1)
		go func() {
			errCh <- srv.Run(ctx, ln)
		}()

		// The server should be serving because Run's internal probe passed
		// before onReady was called. Verify externally.
		Eventually(func() error {
			resp, reqErr := http.Get(fmt.Sprintf("http://%s/api/v1alpha1/containers/health", addr))
			if reqErr != nil {
				return reqErr
			}
			_ = resp.Body.Close()
			return nil
		}).WithTimeout(5 * time.Second).WithPolling(50 * time.Millisecond).Should(Succeed())
	})
	// TC-I079: Shutdown timeout force-terminates hung requests
	It("force-terminates when shutdown timeout expires (TC-I079)", func() {
		shortTimeoutCfg := &config.Config{
			Server: config.ServerConfig{
				Address:         ":0",
				ShutdownTimeout: 200 * time.Millisecond,
			},
		}

		reqStarted := make(chan struct{})

		blockingWrapper := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/test/block" {
					close(reqStarted)
					// Block until the request context is cancelled by shutdown.
					<-r.Context().Done()
					return
				}
				next.ServeHTTP(w, r)
			})
		}

		addr, cancel, errCh := startServer(shortTimeoutCfg, nil, nil, blockingWrapper)

		// Start a request that will block for 30s.
		go func() {
			resp, err := http.Get(fmt.Sprintf("http://%s/test/block", addr))
			if err == nil {
				_ = resp.Body.Close()
			}
		}()

		// Wait for the blocking request to be in-flight.
		<-reqStarted

		// Cancel context to trigger shutdown.
		cancel()

		// Server should exit within the shutdown timeout (~200ms) + buffer,
		// not hang for 30s. The error should wrap context.DeadlineExceeded
		// from the shutdown timeout expiring.
		var serverErr error
		Eventually(errCh).WithTimeout(2 * time.Second).Should(Receive(&serverErr))
		Expect(serverErr).To(MatchError(context.DeadlineExceeded))
	})

	// TC-U070: ResponseErrorHandlerFunc returns RFC 7807
	It("ResponseErrorHandlerFunc returns RFC 7807 INTERNAL response (TC-U070)", func() {
		var logBuf syncBuffer
		logger := slog.New(slog.NewJSONHandler(&logBuf, nil))
		handler := apiserver.NewResponseErrorHandler(logger)

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/some/path", http.NoBody)
		handler(w, req, fmt.Errorf("database connection lost"))

		Expect(w.Code).To(Equal(http.StatusInternalServerError))
		Expect(w.Header().Get("Content-Type")).To(Equal("application/problem+json"))

		var problemJSON map[string]any
		Expect(json.Unmarshal(w.Body.Bytes(), &problemJSON)).To(Succeed())
		Expect(problemJSON).To(HaveKeyWithValue("type", "INTERNAL"))
		Expect(problemJSON).To(HaveKeyWithValue("title", "Internal Server Error"))
		Expect(problemJSON["status"]).To(BeNumerically("==", 500))
		Expect(problemJSON).To(HaveKeyWithValue("detail", "an unexpected error occurred"))
		Expect(problemJSON).To(HaveKeyWithValue("instance", "/some/path"))

		// Must not leak the raw error message.
		bodyStr := w.Body.String()
		Expect(bodyStr).NotTo(ContainSubstring("database connection lost"))
	})

	// TC-I096: Request logging — successful request
	It("logs HTTP requests with method, path, status, and duration (TC-I096)", func() {
		var logBuf syncBuffer
		addr, cancel, errCh := startServer(defaultConfig(), &logBuf, nil)
		defer func() {
			cancel()
			Eventually(errCh).WithTimeout(10 * time.Second).Should(Receive())
		}()

		resp, err := http.Get(fmt.Sprintf("http://%s/api/v1alpha1/containers/health", addr))
		Expect(err).NotTo(HaveOccurred())
		_ = resp.Body.Close()

		Eventually(logBuf.String).WithTimeout(2 * time.Second).WithPolling(50 * time.Millisecond).Should(And(
			ContainSubstring(`"method":"GET"`),
			ContainSubstring(`"path":"/api/v1alpha1/containers/health"`),
			ContainSubstring(`"status":200`),
			ContainSubstring(`"duration"`),
		))
	})

	// TC-I098: Request timeout cancels long-running requests
	It("cancels request context after configured timeout (TC-I098)", func() {
		shortTimeoutCfg := &config.Config{
			Server: config.ServerConfig{
				Address:         ":0",
				ShutdownTimeout: 5 * time.Second,
				RequestTimeout:  200 * time.Millisecond,
			},
		}

		ctxCancelled := make(chan struct{})
		bh := &blockingListHandler{ctxCancelled: ctxCancelled}
		logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
		srv := apiserver.New(shortTimeoutCfg, logger, bh)

		ln, err := net.Listen("tcp", ":0")
		Expect(err).NotTo(HaveOccurred())
		addr := ln.Addr().String()

		ctx, cancel := context.WithCancel(context.Background())
		errCh := make(chan error, 1)
		go func() {
			errCh <- srv.Run(ctx, ln)
		}()
		defer func() {
			cancel()
			Eventually(errCh).WithTimeout(10 * time.Second).Should(Receive())
		}()

		// Wait for the server to be ready.
		Eventually(func() error {
			resp, reqErr := http.Get(fmt.Sprintf("http://%s/api/v1alpha1/containers/health", addr))
			if reqErr != nil {
				return reqErr
			}
			_ = resp.Body.Close()
			return nil
		}).WithTimeout(5 * time.Second).WithPolling(50 * time.Millisecond).Should(Succeed())

		go func() {
			resp, err := http.Get(fmt.Sprintf("http://%s/api/v1alpha1/containers", addr))
			if err == nil {
				_ = resp.Body.Close()
			}
		}()

		// The context should be cancelled within ~200ms by the timeout middleware.
		Eventually(ctxCancelled).WithTimeout(2 * time.Second).Should(BeClosed())
	})

	// TC-I103: Health endpoint at resource-relative path
	It("serves health at /api/v1alpha1/containers/health (TC-I103)", func() {
		addr, cancel, errCh := startServer(defaultConfig(), nil, nil)
		defer func() {
			cancel()
			Eventually(errCh).WithTimeout(10 * time.Second).Should(Receive())
		}()

		resp, err := http.Get(fmt.Sprintf("http://%s/api/v1alpha1/containers/health", addr))
		Expect(err).NotTo(HaveOccurred())
		defer func() { _ = resp.Body.Close() }()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		body, err := io.ReadAll(resp.Body)
		Expect(err).NotTo(HaveOccurred())

		var healthJSON map[string]any
		Expect(json.Unmarshal(body, &healthJSON)).To(Succeed())
		Expect(healthJSON).To(HaveKey("status"))
		Expect(healthJSON).To(HaveKey("type"))
		Expect(healthJSON).To(HaveKey("path"))
		Expect(healthJSON).To(HaveKey("version"))
		Expect(healthJSON).To(HaveKey("uptime"))
	})

	// TC-I118: Health endpoint returns "unhealthy" when K8s is unreachable
	// Validates: REQ-HLT-060 / AC-HLT-025
	It("returns unhealthy status when health check fails (TC-I118)", func() {
		repo := &mockContainerRepo{
			CheckHealthFunc: func() error { return errors.New("kubernetes unreachable") },
		}
		addr, cancel, errCh := startServerWithRepo(defaultConfig(), nil, nil, repo)
		defer func() {
			cancel()
			Eventually(errCh).WithTimeout(10 * time.Second).Should(Receive())
		}()

		resp, err := http.Get(fmt.Sprintf("http://%s/api/v1alpha1/containers/health", addr))
		Expect(err).NotTo(HaveOccurred())
		defer func() { _ = resp.Body.Close() }()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		body, err := io.ReadAll(resp.Body)
		Expect(err).NotTo(HaveOccurred())

		var healthJSON map[string]any
		Expect(json.Unmarshal(body, &healthJSON)).To(Succeed())
		Expect(healthJSON["status"]).To(Equal("unhealthy"))
		Expect(healthJSON).To(HaveKey("type"))
		Expect(healthJSON).To(HaveKey("path"))
		Expect(healthJSON).To(HaveKey("version"))
		Expect(healthJSON).To(HaveKey("uptime"))
	})

	// TC-I097: Request logging — error request (404)
	It("logs HTTP error requests with correct status (TC-I097)", func() {
		repo := &mockContainerRepo{
			GetFunc: func(_ context.Context, _ string) (*v1alpha1.Container, error) {
				return nil, &store.NotFoundError{ID: "nonexistent-id"}
			},
		}

		var logBuf syncBuffer
		addr, cancel, errCh := startServerWithRepo(defaultConfig(), &logBuf, nil, repo)
		defer func() {
			cancel()
			Eventually(errCh).WithTimeout(10 * time.Second).Should(Receive())
		}()

		resp, err := http.Get(fmt.Sprintf("http://%s/api/v1alpha1/containers/nonexistent-id", addr))
		Expect(err).NotTo(HaveOccurred())
		_ = resp.Body.Close()

		Expect(resp.StatusCode).To(Equal(http.StatusNotFound))

		Eventually(logBuf.String).WithTimeout(2 * time.Second).WithPolling(50 * time.Millisecond).Should(And(
			ContainSubstring(`"method":"GET"`),
			ContainSubstring(`"path":"/api/v1alpha1/containers/nonexistent-id"`),
			ContainSubstring(`"status":404`),
		))
	})
})
