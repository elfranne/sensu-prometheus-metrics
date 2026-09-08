package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	corev2 "github.com/sensu/core/v2"
	"github.com/sensu/sensu-plugin-sdk/sensu"
)

// exposition is a minimal but representative exporter payload: one untyped
// gauge without labels and a counter with two label sets.
const exposition = `# HELP go_goroutines Number of goroutines that currently exist.
# TYPE go_goroutines gauge
go_goroutines 42
# HELP http_requests_total Total number of HTTP requests.
# TYPE http_requests_total counter
http_requests_total{code="200",method="get"} 1027
http_requests_total{code="500",method="get"} 3
`

// withPlugin snapshots the package level plugin config and restores it when the
// test ends. QueryExporter reads plugin.Labels directly, so tests that touch
// the globals must not run in parallel.
func withPlugin(t *testing.T) {
	t.Helper()
	saved := plugin
	t.Cleanup(func() { plugin = saved })
}

// newExporter starts a plain HTTP exporter serving body.
func newExporter(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// findSample returns the sample whose label set matches want.
func findSample(t *testing.T, samples model.Vector, want model.Metric) *model.Sample {
	t.Helper()
	for _, sample := range samples {
		if sample.Metric.Equal(want) {
			return sample
		}
	}
	t.Fatalf("no sample matching %s, got %s", want, samples)
	return nil
}

func TestQueryExporter(t *testing.T) {
	withPlugin(t)
	srv := newExporter(t, exposition)

	samples, err := QueryExporter(srv.URL, nil, "", "", false, "", "", "")
	if err != nil {
		t.Fatalf("QueryExporter() error = %v", err)
	}
	if len(samples) != 3 {
		t.Fatalf("got %d samples, want 3: %s", len(samples), samples)
	}

	tests := []struct {
		metric model.Metric
		value  model.SampleValue
	}{
		{model.Metric{"__name__": "go_goroutines"}, 42},
		{model.Metric{"__name__": "http_requests_total", "code": "200", "method": "get"}, 1027},
		{model.Metric{"__name__": "http_requests_total", "code": "500", "method": "get"}, 3},
	}
	for _, test := range tests {
		if got := findSample(t, samples, test.metric).Value; got != test.value {
			t.Errorf("%s = %v, want %v", test.metric, got, test.value)
		}
	}
}

func TestQueryExporterAddsLabels(t *testing.T) {
	withPlugin(t)
	srv := newExporter(t, exposition)

	// Whitespace around the name and the value is trimmed, and a value may
	// itself contain a colon.
	plugin.Labels = []string{"env: production", " dc :eu-west-1", "url:http://example.com:9090"}

	samples, err := QueryExporter(srv.URL, plugin.Labels, "", "", false, "", "", "")
	if err != nil {
		t.Fatalf("QueryExporter() error = %v", err)
	}
	if len(samples) == 0 {
		t.Fatal("no samples returned")
	}

	want := map[model.LabelName]model.LabelValue{
		"env": "production",
		"dc":  "eu-west-1",
		"url": "http://example.com:9090",
	}
	for _, sample := range samples {
		for name, value := range want {
			if got := sample.Metric[name]; got != value {
				t.Errorf("%s: label %q = %q, want %q", sample.Metric, name, got, value)
			}
		}
	}
}

func TestQueryExporterLabelOverwritesExporterLabel(t *testing.T) {
	withPlugin(t)
	srv := newExporter(t, exposition)

	plugin.Labels = []string{"code:overridden"}

	samples, err := QueryExporter(srv.URL, plugin.Labels, "", "", false, "", "", "")
	if err != nil {
		t.Fatalf("QueryExporter() error = %v", err)
	}
	for _, sample := range samples {
		if got := sample.Metric["code"]; got != "overridden" {
			t.Errorf("%s: label \"code\" = %q, want \"overridden\"", sample.Metric, got)
		}
	}
}

func TestQueryExporterTimestampIsUnixMilli(t *testing.T) {
	withPlugin(t)
	srv := newExporter(t, exposition)

	before := time.Now().UnixMilli()
	samples, err := QueryExporter(srv.URL, nil, "", "", false, "", "", "")
	if err != nil {
		t.Fatalf("QueryExporter() error = %v", err)
	}
	after := time.Now().UnixMilli()

	for _, sample := range samples {
		got := int64(sample.Timestamp)
		if got < before || got > after {
			t.Errorf("%s: timestamp = %d, want within [%d, %d]", sample.Metric, got, before, after)
		}
	}
}

func TestQueryExporterBasicAuth(t *testing.T) {
	tests := []struct {
		name         string
		user         string
		password     string
		wantAuth     bool
		wantUser     string
		wantPassword string
	}{
		{name: "credentials sent", user: "sensu", password: "s3cret", wantAuth: true, wantUser: "sensu", wantPassword: "s3cret"},
		{name: "no credentials", wantAuth: false},
		{name: "user without password", user: "sensu", wantAuth: false},
		{name: "password without user", password: "s3cret", wantAuth: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			withPlugin(t)

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				user, password, ok := r.BasicAuth()
				if ok != test.wantAuth {
					t.Errorf("basic auth present = %v, want %v", ok, test.wantAuth)
				}
				if ok && (user != test.wantUser || password != test.wantPassword) {
					t.Errorf("basic auth = %q:%q, want %q:%q", user, password, test.wantUser, test.wantPassword)
				}
				_, _ = io.WriteString(w, exposition)
			}))
			t.Cleanup(srv.Close)

			if _, err := QueryExporter(srv.URL, nil, test.user, test.password, false, "", "", ""); err != nil {
				t.Fatalf("QueryExporter() error = %v", err)
			}
		})
	}
}

func TestQueryExporterErrors(t *testing.T) {
	withPlugin(t)

	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down for maintenance", http.StatusServiceUnavailable)
	}))
	t.Cleanup(failing.Close)

	malformed := newExporter(t, "this is not { valid exposition\n")

	unreachable := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	unreachableURL := unreachable.URL
	unreachable.Close()

	tests := []struct {
		name    string
		url     string
		wantErr string
	}{
		{name: "non OK status", url: failing.URL, wantErr: "non OK HTTP response status: 503"},
		{name: "malformed body", url: malformed.URL, wantErr: "text format parsing error"},
		{name: "connection refused", url: unreachableURL},
		{name: "invalid url", url: "://not a url"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			samples, err := QueryExporter(test.url, nil, "", "", false, "", "", "")
			if err == nil {
				t.Fatalf("QueryExporter() error = nil, want an error (got %s)", samples)
			}
			if samples != nil {
				t.Errorf("QueryExporter() samples = %s, want nil", samples)
			}
			if test.wantErr != "" && !strings.Contains(err.Error(), test.wantErr) {
				t.Errorf("QueryExporter() error = %q, want it to contain %q", err, test.wantErr)
			}
		})
	}
}

func TestQueryExporterInsecureSkipVerify(t *testing.T) {
	tests := []struct {
		name     string
		insecure bool
		wantErr  bool
	}{
		{name: "verified against system roots", insecure: false, wantErr: true},
		{name: "verification skipped", insecure: true, wantErr: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			withPlugin(t)

			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, exposition)
			}))
			t.Cleanup(srv.Close)

			_, err := QueryExporter(srv.URL, nil, "", "", test.insecure, "", "", "")
			if (err != nil) != test.wantErr {
				t.Fatalf("QueryExporter() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestQueryExporterMutualTLS(t *testing.T) {
	withPlugin(t)

	ca := newCA(t)
	serverCert := ca.issue(t, "server", x509.ExtKeyUsageServerAuth)
	clientCert := ca.issue(t, "client", x509.ExtKeyUsageClientAuth)

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.TLS.PeerCertificates) == 0 {
			t.Error("server received no client certificate")
		}
		_, _ = io.WriteString(w, exposition)
	}))
	srv.TLS = &tls.Config{
		Certificates: []tls.Certificate{serverCert.keyPair(t)},
		ClientCAs:    ca.pool(),
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	samples, err := QueryExporter(srv.URL, nil, "", "", false, clientCert.certPath, clientCert.keyPath, ca.certPath)
	if err != nil {
		t.Fatalf("QueryExporter() error = %v", err)
	}
	if len(samples) != 3 {
		t.Fatalf("got %d samples, want 3: %s", len(samples), samples)
	}

	t.Run("client certificate rejected without a trusted CA", func(t *testing.T) {
		other := newCA(t)
		if _, err := QueryExporter(srv.URL, nil, "", "", false, clientCert.certPath, clientCert.keyPath, other.certPath); err == nil {
			t.Fatal("QueryExporter() error = nil, want a certificate verification error")
		}
	})
}

func TestQueryExporterUnreadableCertificates(t *testing.T) {
	withPlugin(t)

	ca := newCA(t)
	client := ca.issue(t, "client", x509.ExtKeyUsageClientAuth)
	missing := filepath.Join(t.TempDir(), "missing.pem")
	srv := newExporter(t, exposition)

	tests := []struct {
		name             string
		cert, key, cacer string
	}{
		{name: "missing cert", cert: missing, key: client.keyPath, cacer: ca.certPath},
		{name: "missing key", cert: client.certPath, key: missing, cacer: ca.certPath},
		{name: "missing ca", cert: client.certPath, key: client.keyPath, cacer: missing},
		{name: "ca only", cacer: ca.certPath},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			samples, err := QueryExporter(srv.URL, nil, "", "", false, test.cert, test.key, test.cacer)
			if err == nil {
				t.Fatalf("QueryExporter() error = nil, want an error (got %s)", samples)
			}
			if samples != nil {
				t.Errorf("QueryExporter() samples = %s, want nil", samples)
			}
		})
	}
}

func TestCheckArgs(t *testing.T) {
	status, err := checkArgs(corev2.FixtureEvent("entity", "check"))
	if err != nil {
		t.Fatalf("checkArgs() error = %v", err)
	}
	if status != sensu.CheckStateOK {
		t.Errorf("checkArgs() status = %d, want %d", status, sensu.CheckStateOK)
	}
}

func TestExecuteCheck(t *testing.T) {
	withPlugin(t)
	srv := newExporter(t, exposition)
	plugin.Url = srv.URL
	plugin.Labels = []string{"env:production"}

	var status int
	var err error
	output := captureStdout(t, func() {
		status, err = executeCheck(corev2.FixtureEvent("entity", "check"))
	})

	if err != nil {
		t.Fatalf("executeCheck() error = %v", err)
	}
	if status != sensu.CheckStateOK {
		t.Fatalf("executeCheck() status = %d, want %d", status, sensu.CheckStateOK)
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d output lines, want 3:\n%s", len(lines), output)
	}
	for _, line := range lines {
		// Sensu's prometheus_text format: <metric>{<labels>} <value> <timestamp>
		fields := strings.Fields(line)
		if len(fields) < 3 {
			t.Errorf("line %q does not have a name, a value and a timestamp", line)
			continue
		}
		if !strings.Contains(line, `env="production"`) {
			t.Errorf("line %q is missing the configured label", line)
		}
	}
}

func TestExecuteCheckUnknownOnFailure(t *testing.T) {
	withPlugin(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	plugin.Url = srv.URL

	var status int
	var err error
	output := captureStdout(t, func() {
		status, err = executeCheck(corev2.FixtureEvent("entity", "check"))
	})

	// The check reports UNKNOWN rather than returning an error, so the SDK
	// prints the reason itself instead of the plugin's usage text.
	if err != nil {
		t.Fatalf("executeCheck() error = %v, want nil", err)
	}
	if status != sensu.CheckStateUnknown {
		t.Errorf("executeCheck() status = %d, want %d", status, sensu.CheckStateUnknown)
	}
	if !strings.Contains(output, "Failed:") {
		t.Errorf("output = %q, want it to explain the failure", output)
	}
}

// captureStdout collects everything fn writes to os.Stdout.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	original := os.Stdout
	os.Stdout = writer

	collected := make(chan string, 1)
	go func() {
		out, _ := io.ReadAll(reader)
		collected <- string(out)
	}()

	defer func() {
		os.Stdout = original
		_ = writer.Close()
		_ = reader.Close()
	}()
	fn()
	os.Stdout = original
	if err := writer.Close(); err != nil {
		t.Fatalf("closing the pipe: %v", err)
	}
	return <-collected
}

// testCert is a certificate and its key, on disk in PEM form.
type testCert struct {
	certPath string
	keyPath  string
	template *x509.Certificate
	key      *ecdsa.PrivateKey
}

func (c testCert) keyPair(t *testing.T) tls.Certificate {
	t.Helper()
	pair, err := tls.LoadX509KeyPair(c.certPath, c.keyPath)
	if err != nil {
		t.Fatalf("tls.LoadX509KeyPair() error = %v", err)
	}
	return pair
}

// testCA is a throwaway certificate authority backed by files in a temp dir.
type testCA struct {
	testCert
	cert *x509.Certificate
	dir  string
}

func (ca testCA) pool() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(ca.cert)
	return pool
}

func newCA(t *testing.T) testCA {
	t.Helper()

	dir := t.TempDir()
	key := newKey(t)
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "sensu-prometheus-metrics test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der := signCert(t, template, template, &key.PublicKey, key)
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("x509.ParseCertificate() error = %v", err)
	}

	ca := testCA{cert: cert, dir: dir}
	ca.certPath, ca.keyPath = writePEM(t, dir, "ca", der, key)
	ca.template, ca.key = template, key
	return ca
}

// issue signs a leaf certificate valid for 127.0.0.1 and localhost.
func (ca testCA) issue(t *testing.T, name string, usage x509.ExtKeyUsage) testCert {
	t.Helper()

	key := newKey(t)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.IPv6loopback},
	}
	der := signCert(t, template, ca.cert, &key.PublicKey, ca.key)

	leaf := testCert{template: template, key: key}
	leaf.certPath, leaf.keyPath = writePEM(t, ca.dir, name, der, key)
	return leaf
}

func newKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey() error = %v", err)
	}
	return key
}

func signCert(t *testing.T, template, parent *x509.Certificate, pub *ecdsa.PublicKey, signer *ecdsa.PrivateKey) []byte {
	t.Helper()
	der, err := x509.CreateCertificate(rand.Reader, template, parent, pub, signer)
	if err != nil {
		t.Fatalf("x509.CreateCertificate() error = %v", err)
	}
	return der
}

func writePEM(t *testing.T, dir, name string, der []byte, key *ecdsa.PrivateKey) (certPath, keyPath string) {
	t.Helper()

	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("x509.MarshalPKCS8PrivateKey() error = %v", err)
	}

	certPath = filepath.Join(dir, name+".pem")
	keyPath = filepath.Join(dir, name+"-key.pem")
	write := func(path, blockType string, bytes []byte) {
		if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: bytes}), 0o600); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}
	}
	write(certPath, "CERTIFICATE", der)
	write(keyPath, "PRIVATE KEY", keyDER)
	return certPath, keyPath
}
