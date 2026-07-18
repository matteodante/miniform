package stress_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/matteodante/miniform/internal/database"
	"github.com/matteodante/miniform/internal/forms"
	"github.com/matteodante/miniform/internal/pkg/dbtxn"
)

const (
	mebibyte        = 1024 * 1024
	uploadSize      = 64 * 1024
	stressUserAgent = "Miniform stress test"
)

type stressConfig struct {
	requests        int
	concurrency     int
	idleDuration    time.Duration
	maxIdleRSSMiB   int
	maxPeakRSSMiB   int
	maxIdleCPU      int
	maxRSSGrowthMiB int
}

type requestPlan struct {
	sequence int
	form     *forms.Form
	valid    bool
	delivery bool
	upload   bool
	json     bool
}

type requestResult struct {
	id      uint
	latency time.Duration
	err     error
}

type loadResult struct {
	valid, invalid, deliveries, uploads int
	ids                                 map[uint]struct{}
	sequences                           map[int]int
	latencies                           []time.Duration
}

type storageSnapshot struct {
	submissions, files, webhookEvents, emailEvents int
	delivered, outstanding, failed                 int
	sequences                                      map[int]int
	dataBytes, walBytes                            int64
}

func TestCoreStress(t *testing.T) {
	if os.Getenv("MINIFORM_RUN_STRESS") != "1" {
		t.Skip("set MINIFORM_RUN_STRESS=1 or run make test-stress")
	}

	t.Run("preserves ingestion delivery storage and lifecycle under load", func(t *testing.T) {
		if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
			t.Skip("live process resource sampling supports macOS and Linux")
		}
		cfg := loadStressConfig(t)
		binary := strings.TrimSpace(os.Getenv("MINIFORM_STRESS_BINARY"))
		require.NotEmpty(t, binary, "MINIFORM_STRESS_BINARY is required")
		info, err := os.Stat(binary)
		require.NoError(t, err)
		require.False(t, info.IsDir())

		dataDir := t.TempDir()
		databasePath := filepath.Join(dataDir, "miniform.db")
		capture := newWebhookCapture(t)
		loadForm, deliveryForm := seedStressForms(t, databasePath, capture.server.URL)
		port := availablePort(t)
		baseURL := "http://127.0.0.1:" + port

		startedAt := time.Now()
		process := startServer(t, binary, port, dataDir, databasePath)
		t.Cleanup(process.cleanup)
		sampler, err := startResourceSampler(process.cmd.Process.Pid)
		require.NoError(t, err)
		t.Cleanup(sampler.stop)
		waitForHealth(t, process, baseURL)
		require.NoError(t, sampler.capture())
		readyAt := time.Now()

		idleStartedAt := time.Now()
		require.NoError(t, sampler.capture())
		time.Sleep(cfg.idleDuration)
		require.NoError(t, sampler.capture())
		idleEndedAt := time.Now()

		plans := make([]requestPlan, cfg.requests)
		for sequence := range plans {
			plans[sequence] = planRequest(sequence, loadForm, deliveryForm)
		}
		client, closeClient := stressClient(cfg.concurrency)
		defer closeClient()
		loadStartedAt := time.Now()
		require.NoError(t, sampler.capture())
		results := runLoad(t.Context(), client, baseURL, cfg.concurrency, plans)
		require.NoError(t, sampler.capture())
		loadEndedAt := time.Now()
		load := validateLoad(t, plans, results)

		drainStartedAt := time.Now()
		waitForWebhookCount(t, process, capture, load.deliveries, 30*time.Second)
		drainEndedAt := time.Now()

		postStartedAt := time.Now()
		require.NoError(t, sampler.capture())
		time.Sleep(cfg.idleDuration)
		require.NoError(t, sampler.capture())
		postEndedAt := time.Now()

		blocked, release := capture.blockNext()
		defer release()
		interrupted := requestPlan{
			sequence: cfg.requests, form: deliveryForm, valid: true, delivery: true,
		}
		interruptedResult := performRequest(t.Context(), client, baseURL, interrupted)
		require.NoError(t, interruptedResult.err)
		load.ids[interruptedResult.id] = struct{}{}
		load.sequences[interrupted.sequence]++
		select {
		case <-blocked:
		case <-time.After(10 * time.Second):
			t.Fatal("background runner did not start the blocked webhook")
		}
		closeClient()

		shutdownDuration, err := process.stop()
		require.NoError(t, err)
		shutdownEndedAt := time.Now()
		release()
		sampler.stop()

		startupResources := requireResources(t, sampler, startedAt, readyAt)
		idleResources := requireResources(t, sampler, idleStartedAt, idleEndedAt)
		loadResources := requireResources(t, sampler, loadStartedAt, loadEndedAt)
		postResources := requireResources(t, sampler, postStartedAt, postEndedAt)
		allResources := requireResources(t, sampler, startedAt, shutdownEndedAt)
		validateResourceBudgets(t, cfg, idleResources, postResources, allResources)

		expectedSubmissions := load.valid + 1
		expectedDeliveries := load.deliveries + 1
		firstSnapshot := inspectStorage(t, databasePath, dataDir)
		assert.Equal(t, expectedSubmissions, firstSnapshot.submissions)
		assert.Equal(t, load.uploads, firstSnapshot.files)
		assert.Equal(t, expectedDeliveries, firstSnapshot.webhookEvents)
		assert.Zero(t, firstSnapshot.emailEvents)
		assert.Equal(t, load.deliveries, firstSnapshot.delivered)
		assert.Equal(t, 1, firstSnapshot.outstanding)
		assert.Zero(t, firstSnapshot.failed)
		assert.Equal(t, load.sequences, firstSnapshot.sequences)
		assert.Zero(t, firstSnapshot.walBytes)
		require.Len(t, load.ids, expectedSubmissions)

		expireOutstandingWebhook(t, databasePath)
		restarted := startServer(t, binary, port, dataDir, databasePath)
		t.Cleanup(restarted.cleanup)
		waitForHealth(t, restarted, baseURL)
		waitForWebhookCount(t, restarted, capture, expectedDeliveries+1, 10*time.Second)
		time.Sleep(250 * time.Millisecond)
		_, err = restarted.stop()
		require.NoError(t, err)

		finalSnapshot := inspectStorage(t, databasePath, dataDir)
		assert.Equal(t, expectedSubmissions, finalSnapshot.submissions)
		assert.Equal(t, load.uploads, finalSnapshot.files)
		assert.Equal(t, expectedDeliveries, finalSnapshot.webhookEvents)
		assert.Equal(t, expectedDeliveries, finalSnapshot.delivered)
		assert.Zero(t, finalSnapshot.outstanding)
		assert.Zero(t, finalSnapshot.failed)
		assert.Equal(t, load.sequences, finalSnapshot.sequences)
		assert.Zero(t, finalSnapshot.walBytes)

		webhooks := capture.snapshot()
		assert.Empty(t, webhooks.errors)
		assert.Zero(t, webhooks.emptyKeys)
		assert.Equal(t, expectedDeliveries, len(webhooks.keys))
		assert.Equal(t, expectedDeliveries+1, webhooks.requests)
		assert.Equal(t, 1, webhooks.duplicateKeys)

		latencies := append([]time.Duration(nil), load.latencies...)
		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		throughput := float64(len(plans)) / loadEndedAt.Sub(loadStartedAt).Seconds()
		t.Logf("profile: requests=%d valid=%d invalid=%d uploads=%d deliveries=%d concurrency=%d throughput=%.1f req/s",
			len(plans), load.valid, load.invalid, load.uploads, load.deliveries, cfg.concurrency, throughput)
		t.Logf("latency: p50=%s p95=%s p99=%s", percentile(latencies, 50), percentile(latencies, 95), percentile(latencies, 99))
		t.Logf("lifecycle: startup=%s queue_drain=%s shutdown_under_load=%s",
			readyAt.Sub(startedAt), drainEndedAt.Sub(drainStartedAt), shutdownDuration)
		t.Logf("resources: startup=%s idle=%s load=%s post_load=%s peak_rss=%.1fMiB disk=%.1fMiB",
			startupResources, idleResources, loadResources, postResources,
			allResources.peakRSSMiB, float64(finalSnapshot.dataBytes)/mebibyte)
	})
}

func loadStressConfig(t *testing.T) stressConfig {
	t.Helper()
	cfg := stressConfig{
		requests:        positiveEnv(t, "MINIFORM_STRESS_REQUESTS", 500),
		concurrency:     positiveEnv(t, "MINIFORM_STRESS_CONCURRENCY", 16),
		idleDuration:    time.Duration(positiveEnv(t, "MINIFORM_STRESS_IDLE_SECONDS", 3)) * time.Second,
		maxIdleRSSMiB:   positiveEnv(t, "MINIFORM_STRESS_MAX_IDLE_RSS_MB", 128),
		maxPeakRSSMiB:   positiveEnv(t, "MINIFORM_STRESS_MAX_PEAK_RSS_MB", 256),
		maxIdleCPU:      positiveEnv(t, "MINIFORM_STRESS_MAX_IDLE_CPU_PERCENT", 10),
		maxRSSGrowthMiB: positiveEnv(t, "MINIFORM_STRESS_MAX_POST_RSS_GROWTH_MB", 64),
	}
	require.GreaterOrEqual(t, cfg.requests, 50)
	require.LessOrEqual(t, cfg.concurrency, cfg.requests)
	return cfg
}

func positiveEnv(t *testing.T, name string, fallback int) int {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	require.NoError(t, err, "%s must be a positive integer", name)
	require.Positive(t, value, "%s must be a positive integer", name)
	return value
}

func seedStressForms(t *testing.T, databasePath, webhookURL string) (*forms.Form, *forms.Form) {
	t.Helper()
	manager := database.NewManager(databasePath, 10, 5)
	db, err := manager.Connect()
	require.NoError(t, err)
	require.NoError(t, database.Migrate(db))
	logger := slog.New(slog.DiscardHandler)
	loadForm, err := forms.Create(logger, db, forms.CreateParams{
		Name: "Stress ingestion", Slug: "stress-ingestion", AllowedOrigins: "*",
	})
	require.NoError(t, err)
	deliveryForm, err := forms.Create(logger, db, forms.CreateParams{
		Name: "Stress delivery", Slug: "stress-delivery", AllowedOrigins: "*",
		WebhookEnabled: true, WebhookURL: webhookURL,
	})
	require.NoError(t, err)
	require.NoError(t, manager.Close())
	return loadForm, deliveryForm
}

func planRequest(sequence int, loadForm, deliveryForm *forms.Form) requestPlan {
	valid := sequence%25 != 0
	delivery := valid && sequence%10 == 0
	form := loadForm
	if delivery {
		form = deliveryForm
	}
	return requestPlan{
		sequence: sequence, form: form, valid: valid, delivery: delivery,
		upload: valid && sequence%11 == 0,
		json:   valid && sequence%11 != 0 && sequence%3 == 0,
	}
}

func stressClient(concurrency int) (*http.Client, func()) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = concurrency
	transport.MaxIdleConnsPerHost = concurrency
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second}
	var once sync.Once
	return client, func() { once.Do(transport.CloseIdleConnections) }
}

func runLoad(ctx context.Context, client *http.Client, baseURL string, concurrency int, plans []requestPlan) []requestResult {
	jobs := make(chan int)
	results := make([]requestResult, len(plans))
	var workers sync.WaitGroup
	workers.Add(concurrency)
	for range concurrency {
		go func() {
			defer workers.Done()
			for index := range jobs {
				results[index] = performRequest(ctx, client, baseURL, plans[index])
			}
		}()
	}
	for index := range plans {
		jobs <- index
	}
	close(jobs)
	workers.Wait()
	return results
}

func performRequest(ctx context.Context, client *http.Client, baseURL string, plan requestPlan) requestResult {
	request, err := buildRequest(ctx, baseURL, plan)
	if err != nil {
		return requestResult{err: err}
	}
	startedAt := time.Now()
	response, err := client.Do(request)
	latency := time.Since(startedAt)
	if err != nil {
		return requestResult{latency: latency, err: err}
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, mebibyte))
	if err != nil {
		return requestResult{latency: latency, err: err}
	}
	expectedStatus := http.StatusOK
	if !plan.valid {
		expectedStatus = http.StatusUnauthorized
	}
	if response.StatusCode != expectedStatus {
		return requestResult{latency: latency, err: fmt.Errorf("sequence %d returned %d: %s", plan.sequence, response.StatusCode, body)}
	}
	if !plan.valid {
		return requestResult{latency: latency}
	}
	var result struct {
		OK           bool `json:"ok"`
		SubmissionID uint `json:"submission_id"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return requestResult{latency: latency, err: fmt.Errorf("decode sequence %d response: %w", plan.sequence, err)}
	}
	if !result.OK || result.SubmissionID == 0 {
		return requestResult{latency: latency, err: fmt.Errorf("sequence %d returned invalid success payload", plan.sequence)}
	}
	return requestResult{id: result.SubmissionID, latency: latency}
}

func buildRequest(ctx context.Context, baseURL string, plan requestPlan) (*http.Request, error) {
	token := plan.form.Token
	if !plan.valid {
		token = "invalid"
	}
	endpoint := fmt.Sprintf("%s/forms/%s/submit?token=%s", baseURL, url.PathEscape(plan.form.Slug), url.QueryEscape(token))
	var body io.Reader
	contentType := "application/x-www-form-urlencoded"
	values := url.Values{"sequence": {strconv.Itoa(plan.sequence)}, "kind": {"stress"}}
	switch {
	case plan.upload:
		var encoded bytes.Buffer
		writer := multipart.NewWriter(&encoded)
		if err := writer.WriteField("sequence", strconv.Itoa(plan.sequence)); err != nil {
			return nil, err
		}
		if err := writer.WriteField("kind", "stress"); err != nil {
			return nil, err
		}
		file, err := writer.CreateFormFile("attachment", fmt.Sprintf("payload-%d.txt", plan.sequence))
		if err != nil {
			return nil, err
		}
		if _, err := file.Write(bytes.Repeat([]byte{'x'}, uploadSize)); err != nil {
			return nil, err
		}
		if err := writer.Close(); err != nil {
			return nil, err
		}
		body = &encoded
		contentType = writer.FormDataContentType()
	case plan.json:
		encoded, err := json.Marshal(map[string]any{"sequence": plan.sequence, "kind": "stress"})
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(encoded)
		contentType = "application/json"
	default:
		body = strings.NewReader(values.Encode())
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("Origin", "https://stress.example")
	request.Header.Set("User-Agent", stressUserAgent)
	request.Header.Set("X-Forwarded-For", fmt.Sprintf("198.18.%d.%d", plan.sequence/250%256, plan.sequence%250+1))
	return request, nil
}

func validateLoad(t *testing.T, plans []requestPlan, results []requestResult) loadResult {
	t.Helper()
	load := loadResult{
		ids: make(map[uint]struct{}), sequences: make(map[int]int),
		latencies: make([]time.Duration, 0, len(results)),
	}
	for index, result := range results {
		require.NoErrorf(t, result.err, "request %d failed", index)
		load.latencies = append(load.latencies, result.latency)
		plan := plans[index]
		if !plan.valid {
			load.invalid++
			continue
		}
		load.valid++
		load.sequences[plan.sequence]++
		if plan.delivery {
			load.deliveries++
		}
		if plan.upload {
			load.uploads++
		}
		if _, duplicate := load.ids[result.id]; duplicate {
			t.Errorf("duplicate submission id %d", result.id)
		}
		load.ids[result.id] = struct{}{}
	}
	return load
}

func percentile(sorted []time.Duration, percentage int) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	index := (len(sorted) - 1) * percentage / 100
	return sorted[index]
}

type serverProcess struct {
	cmd     *exec.Cmd
	done    chan struct{}
	mu      sync.Mutex
	waitErr error
	output  bytes.Buffer
}

func startServer(t *testing.T, binary, port, dataDir, databasePath string) *serverProcess {
	t.Helper()
	process := &serverProcess{done: make(chan struct{})}
	process.cmd = exec.CommandContext(t.Context(), binary)
	process.cmd.Env = append(os.Environ(),
		"MINIFORM_ENV=production",
		"MINIFORM_PORT="+port,
		"MINIFORM_DATA_DIR="+dataDir,
		"MINIFORM_DATABASE_PATH="+databasePath,
		"MINIFORM_LOGS_DIR="+filepath.Join(dataDir, "logs"),
		"MINIFORM_LOG_LEVEL=error",
		"MINIFORM_SESSION_SECRET=stress-secret",
		"MINIFORM_WEBHOOK_BACKOFF_SCHEDULE=1",
		"MATCHA_MANAGER_VERSION=stress",
	)
	process.cmd.Stdout = &process.output
	process.cmd.Stderr = &process.output
	process.cmd.WaitDelay = 2 * time.Second
	require.NoError(t, process.cmd.Start())
	go func() {
		err := process.cmd.Wait()
		process.mu.Lock()
		process.waitErr = err
		process.mu.Unlock()
		close(process.done)
	}()
	return process
}

func (process *serverProcess) stop() (time.Duration, error) {
	startedAt := time.Now()
	select {
	case <-process.done:
		return time.Since(startedAt), process.result()
	default:
	}
	if err := process.cmd.Process.Signal(os.Interrupt); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return 0, err
	}
	select {
	case <-process.done:
		return time.Since(startedAt), process.result()
	case <-time.After(5 * time.Second):
		_ = process.cmd.Process.Kill()
		<-process.done
		return time.Since(startedAt), fmt.Errorf("server did not stop gracefully: %s", process.failure())
	}
}

func (process *serverProcess) result() error {
	process.mu.Lock()
	defer process.mu.Unlock()
	if process.waitErr != nil {
		return fmt.Errorf("server exited: %w: %s", process.waitErr, strings.TrimSpace(process.output.String()))
	}
	return nil
}

func (process *serverProcess) failure() string {
	process.mu.Lock()
	defer process.mu.Unlock()
	return strings.TrimSpace(process.output.String())
}

func (process *serverProcess) cleanup() {
	select {
	case <-process.done:
		return
	default:
	}
	_ = process.cmd.Process.Signal(os.Interrupt)
	select {
	case <-process.done:
	case <-time.After(5 * time.Second):
		_ = process.cmd.Process.Kill()
		<-process.done
	}
}

func waitForHealth(t *testing.T, process *serverProcess, baseURL string) {
	t.Helper()
	transport := &http.Transport{DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 500 * time.Millisecond}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-process.done:
			t.Fatalf("server exited before becoming healthy: %v", process.result())
		default:
		}
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, baseURL+"/_health", nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(request) // #nosec G107 -- loopback stress server.
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("server did not become healthy: %s", process.failure())
}

func availablePort(t *testing.T) string {
	t.Helper()
	listener, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	require.NoError(t, listener.Close())
	return port
}

type processSample struct {
	at               time.Time
	rssMiB, cpuUsage float64
}

type resourceSampler struct {
	pid    int
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
	mu     sync.Mutex
	values []processSample
}

type resourceSummary struct {
	startRSSMiB, endRSSMiB, peakRSSMiB float64
	endCPU, peakCPU                    float64
	samples                            int
}

func (summary resourceSummary) String() string {
	return fmt.Sprintf("rss=%.1f→%.1fMiB peak=%.1fMiB cpu_end=%.1f%% peak=%.1f%% samples=%d",
		summary.startRSSMiB, summary.endRSSMiB, summary.peakRSSMiB,
		summary.endCPU, summary.peakCPU, summary.samples)
}

func startResourceSampler(pid int) (*resourceSampler, error) {
	if _, err := exec.LookPath("ps"); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	sampler := &resourceSampler{pid: pid, ctx: ctx, cancel: cancel, done: make(chan struct{})}
	if err := sampler.capture(); err != nil {
		cancel()
		return nil, err
	}
	go func() {
		defer close(sampler.done)
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = sampler.capture()
			}
		}
	}()
	return sampler, nil
}

func (sampler *resourceSampler) capture() error {
	ctx, cancel := context.WithTimeout(sampler.ctx, time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "ps", "-o", "rss=", "-o", "pcpu=", "-p", strconv.Itoa(sampler.pid))
	command.Env = append(os.Environ(), "LC_ALL=C")
	output, err := command.Output()
	if err != nil {
		return err
	}
	fields := strings.Fields(string(output))
	if len(fields) != 2 {
		return fmt.Errorf("unexpected ps output %q", output)
	}
	rssKiB, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return err
	}
	cpu, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return err
	}
	sampler.mu.Lock()
	sampler.values = append(sampler.values, processSample{at: time.Now(), rssMiB: rssKiB / 1024, cpuUsage: cpu})
	sampler.mu.Unlock()
	return nil
}

func (sampler *resourceSampler) stop() {
	sampler.once.Do(func() {
		sampler.cancel()
		<-sampler.done
	})
}

func (sampler *resourceSampler) summary(start, end time.Time) (resourceSummary, error) {
	sampler.mu.Lock()
	values := append([]processSample(nil), sampler.values...)
	sampler.mu.Unlock()
	sort.Slice(values, func(i, j int) bool { return values[i].at.Before(values[j].at) })
	var selected []processSample
	for _, sample := range values {
		if !sample.at.Before(start) && !sample.at.After(end) {
			selected = append(selected, sample)
		}
	}
	if len(selected) == 0 {
		return resourceSummary{}, fmt.Errorf("no process samples between %s and %s", start, end)
	}
	result := resourceSummary{
		startRSSMiB: selected[0].rssMiB,
		endRSSMiB:   selected[len(selected)-1].rssMiB,
		endCPU:      selected[len(selected)-1].cpuUsage,
		samples:     len(selected),
	}
	for _, sample := range selected {
		result.peakRSSMiB = max(result.peakRSSMiB, sample.rssMiB)
		result.peakCPU = max(result.peakCPU, sample.cpuUsage)
	}
	return result, nil
}

func requireResources(t *testing.T, sampler *resourceSampler, start, end time.Time) resourceSummary {
	t.Helper()
	resources, err := sampler.summary(start, end)
	require.NoError(t, err)
	return resources
}

func validateResourceBudgets(t *testing.T, cfg stressConfig, idle, post, all resourceSummary) {
	t.Helper()
	assert.LessOrEqual(t, idle.peakRSSMiB, float64(cfg.maxIdleRSSMiB), "idle RSS budget exceeded")
	assert.LessOrEqual(t, idle.endCPU, float64(cfg.maxIdleCPU), "idle CPU budget exceeded")
	assert.LessOrEqual(t, all.peakRSSMiB, float64(cfg.maxPeakRSSMiB), "peak RSS budget exceeded")
	growth := max(0.0, post.endRSSMiB-idle.endRSSMiB)
	assert.LessOrEqual(t, growth, float64(cfg.maxRSSGrowthMiB), "post-load RSS growth budget exceeded")
}

type webhookBlock struct {
	started chan struct{}
	release chan struct{}
}

type webhookSnapshot struct {
	requests, emptyKeys, duplicateKeys int
	keys                               map[string]int
	errors                             []string
}

type webhookCapture struct {
	server *httptest.Server
	mu     sync.Mutex
	keys   map[string]int
	errors []string
	block  *webhookBlock
}

func newWebhookCapture(t *testing.T) *webhookCapture {
	t.Helper()
	capture := &webhookCapture{keys: make(map[string]int)}
	capture.server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(io.LimitReader(request.Body, mebibyte))
		var payload map[string]any
		if err == nil {
			err = json.Unmarshal(body, &payload)
		}
		key := request.Header.Get("Idempotency-Key")
		capture.mu.Lock()
		if err != nil || payload["submission"] == nil {
			capture.errors = append(capture.errors, fmt.Sprintf("invalid webhook payload: %v", err))
		}
		capture.keys[key]++
		block := capture.block
		capture.block = nil
		capture.mu.Unlock()
		if block != nil {
			close(block.started)
			<-block.release
		}
		response.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(capture.server.Close)
	return capture
}

func (capture *webhookCapture) blockNext() (<-chan struct{}, func()) {
	block := &webhookBlock{started: make(chan struct{}), release: make(chan struct{})}
	capture.mu.Lock()
	capture.block = block
	capture.mu.Unlock()
	var once sync.Once
	return block.started, func() { once.Do(func() { close(block.release) }) }
}

func (capture *webhookCapture) snapshot() webhookSnapshot {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	result := webhookSnapshot{keys: make(map[string]int), errors: append([]string(nil), capture.errors...)}
	for key, count := range capture.keys {
		result.keys[key] = count
		result.requests += count
		if key == "" {
			result.emptyKeys += count
		}
		if count > 1 {
			result.duplicateKeys++
		}
	}
	return result
}

func waitForWebhookCount(t *testing.T, process *serverProcess, capture *webhookCapture, expected int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if capture.snapshot().requests >= expected {
			return
		}
		select {
		case <-process.done:
			t.Fatalf("server exited while draining webhooks: %v", process.result())
		default:
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("received %d/%d webhooks", capture.snapshot().requests, expected)
}

func inspectStorage(t *testing.T, databasePath, dataDir string) storageSnapshot {
	t.Helper()
	snapshot := storageSnapshot{sequences: make(map[int]int), walBytes: fileSize(databasePath + "-wal")}
	manager := database.NewManager(databasePath, 10, 5)
	db, err := manager.Connect()
	require.NoError(t, err)

	var submissions []forms.Submission
	require.NoError(t, db.Find(&submissions).Error)
	snapshot.submissions = len(submissions)
	for _, submission := range submissions {
		var payload map[string]any
		require.NoError(t, json.Unmarshal([]byte(submission.DataJSON), &payload))
		sequence, err := strconv.Atoi(fmt.Sprint(payload["sequence"]))
		require.NoError(t, err)
		snapshot.sequences[sequence]++
	}

	var files []forms.SubmissionFile
	require.NoError(t, db.Find(&files).Error)
	snapshot.files = len(files)
	for index := range files {
		info, err := os.Stat(filepath.Join(dataDir, files[index].StoragePath))
		require.NoError(t, err)
		assert.Equal(t, files[index].Size, info.Size())
		assert.Equal(t, int64(uploadSize), info.Size())
	}

	snapshot.webhookEvents = rowCount(t, db.Model(&forms.WebhookEvent{}))
	snapshot.emailEvents = rowCount(t, db.Model(&forms.EmailEvent{}))
	snapshot.delivered = rowCount(t, db.Model(&forms.WebhookEvent{}).Where("status = ?", forms.WebhookStatusDelivered))
	snapshot.outstanding = rowCount(t, db.Model(&forms.WebhookEvent{}).Where("status IN ?", []string{
		forms.WebhookStatusPending, forms.WebhookStatusDelivering, forms.WebhookStatusRetrying,
	}))
	snapshot.failed = rowCount(t, db.Model(&forms.WebhookEvent{}).Where("status = ?", forms.WebhookStatusFailed))
	require.NoError(t, manager.Close())

	uploadFiles, err := regularFileCount(filepath.Join(dataDir, "uploads"))
	require.NoError(t, err)
	assert.Equal(t, snapshot.files, uploadFiles)
	for _, staging := range []string{".upload-staging", ".upload-deletions"} {
		count, err := regularFileCount(filepath.Join(dataDir, staging))
		require.NoError(t, err)
		assert.Zero(t, count)
	}
	snapshot.dataBytes, err = directorySize(dataDir)
	require.NoError(t, err)
	return snapshot
}

func rowCount(t *testing.T, query *gorm.DB) int {
	t.Helper()
	var count int64
	require.NoError(t, query.Count(&count).Error)
	return int(count)
}

func expireOutstandingWebhook(t *testing.T, databasePath string) {
	t.Helper()
	manager := database.NewManager(databasePath, 10, 5)
	db, err := manager.Connect()
	require.NoError(t, err)
	var changed int64
	require.NoError(t, dbtxn.WithRetry(slog.New(slog.DiscardHandler), db, func(tx *gorm.DB) error {
		result := tx.Model(&forms.WebhookEvent{}).
			Where("status IN ?", []string{forms.WebhookStatusDelivering, forms.WebhookStatusRetrying}).
			Update("next_attempt_at", time.Now().UTC().Add(-time.Second))
		changed = result.RowsAffected
		return result.Error
	}))
	assert.Equal(t, int64(1), changed)
	require.NoError(t, manager.Close())
}

func regularFileCount(root string) (int, error) {
	count := 0
	err := filepath.WalkDir(root, func(_ string, entry os.DirEntry, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() {
			count++
		}
		return nil
	})
	if os.IsNotExist(err) {
		return 0, nil
	}
	return count, err
}

func directorySize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	return total, err
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}
