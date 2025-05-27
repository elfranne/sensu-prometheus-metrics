package main

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	corev2 "github.com/sensu/core/v2"
	"github.com/sensu/sensu-plugin-sdk/sensu"
)

// Config represents the check plugin config.
type Config struct {
	sensu.PluginConfig
	Url                string
	Labels             []string
	User               string
	Password           string
	Cert               string
	Key                string
	CaCert             string
	insecureSkipVerify bool
}

type Tag struct {
	Name  model.LabelName
	Value model.LabelValue
}
type Metric struct {
	Tags  []Tag
	Value float64
}

var (
	plugin = Config{
		PluginConfig: sensu.PluginConfig{
			Name:     "sensu-prometheus-metrics",
			Short:    "Check metrics from Prometheus",
			Keyspace: "sensu.io/plugins/sensu-prometheus-metrics/config",
		},
	}

	options = []sensu.ConfigOption{
		&sensu.PluginConfigOption[string]{
			Path:     "url",
			Argument: "url",
			Default:  "http://localhost:8405/metrics",
			Usage:    "URL to the Prometheus metrics",
			Value:    &plugin.Url,
		},
		&sensu.SlicePluginConfigOption[string]{
			Path:     "label",
			Argument: "label",
			Usage:    "labels to add to metrics",
			Default:  []string{},
			Value:    &plugin.Labels,
		},
		&sensu.PluginConfigOption[string]{
			Path:     "user",
			Argument: "user",
			Usage:    "User for basic auth",
			Value:    &plugin.User,
		},
		&sensu.PluginConfigOption[string]{
			Path:     "password",
			Argument: "password",
			Usage:    "Password for basic auth",
			Value:    &plugin.Password,
		},
		&sensu.PluginConfigOption[string]{
			Path:     "cert",
			Argument: "cert",
			Usage:    "Cert to use for mTLS",
			Value:    &plugin.Cert,
		},
		&sensu.PluginConfigOption[string]{
			Path:     "key",
			Argument: "key",
			Usage:    "Key to use for mTLS",
			Value:    &plugin.Key,
		},
		&sensu.PluginConfigOption[string]{
			Path:     "cacert",
			Argument: "cacert",
			Usage:    "CA cert to use for mTLS",
			Value:    &plugin.CaCert,
		},
		&sensu.PluginConfigOption[bool]{
			Path:     "insecureskipverify",
			Argument: "insecureskipverify",
			Usage:    "insecureskipverify option if using self signed certs.",
			Value:    &plugin.insecureSkipVerify,
		},
	}
)

func main() {
	check := sensu.NewCheck(&plugin.PluginConfig, options, checkArgs, executeCheck, false)
	check.Execute()
}

func checkArgs(event *corev2.Event) (int, error) {
	return sensu.CheckStateOK, nil
}
func QueryExporter(exporterURL string, Labels []string, user string, password string, insecureSkipVerify bool, cert string, key string, cacert string) (model.Vector, error) {

	tlsconfig := &tls.Config{}

	if insecureSkipVerify {
		tlsconfig = &tls.Config{InsecureSkipVerify: true}
	}

	if len(cert) > 0 || len(key) > 0 || len(cacert) > 0 {
		certpair, err := tls.LoadX509KeyPair(cert, key)
		if err != nil {
			fmt.Printf("could not load certificate(%s) or key(%s): %v", cert, key, err)
			return nil, err
		}

		cacertfile, err := os.ReadFile(cacert)
		if err != nil {
			fmt.Printf("could not load CA(%s): %v", cacert, err)
			return nil, err
		}
		rootca := x509.NewCertPool()
		rootca.AppendCertsFromPEM(cacertfile)
		tlsconfig = &tls.Config{
			Certificates: []tls.Certificate{certpair},
			RootCAs:      rootca,
		}
	}

	tr := &http.Transport{
		TLSClientConfig: tlsconfig,
	}
	client := &http.Client{Transport: tr}
	req, err := http.NewRequest("GET", exporterURL, nil)
	if err != nil {
		return nil, err
	}
	if user != "" && password != "" {
		req.SetBasicAuth(user, password)
	}

	expResponse, err := client.Do(req)
	if expResponse != nil {
		defer expResponse.Body.Close()
	}
	if err != nil {
		return nil, err
	}
	if expResponse.StatusCode != http.StatusOK {
		return nil, errors.New("exporter returned non OK HTTP response status: " + expResponse.Status)
	}
	var parser expfmt.TextParser

	metricFamilies, err := parser.TextToMetricFamilies(expResponse.Body)
	if err != nil {
		return nil, err
	}

	samples := model.Vector{}

	decodeOptions := &expfmt.DecodeOptions{
		Timestamp: model.Time(time.Now().UnixMilli()),
	}

	for _, family := range metricFamilies {
		familySamples, _ := expfmt.ExtractSamples(decodeOptions, family)
		for _, addLabel := range familySamples {
			if len(plugin.Labels) > 0 {
				for _, label := range plugin.Labels {
					labelSplit := strings.SplitN(label, ":", 2)
					labelName := strings.TrimSpace(labelSplit[0])
					labelValue := strings.TrimSpace(labelSplit[1])
					addLabel.Metric[model.LabelName(labelName)] = model.LabelValue(labelValue)
				}
			}
		}
		samples = append(samples, familySamples...)
	}
	return samples, nil
}
func executeCheck(event *corev2.Event) (int, error) {

	var samples model.Vector
	var err error

	samples, err = QueryExporter(plugin.Url, plugin.Labels, plugin.User, plugin.Password, plugin.insecureSkipVerify, plugin.Cert, plugin.Key, plugin.CaCert)
	if err != nil {
		fmt.Printf("Failed: %s\n", err)
		return sensu.CheckStateUnknown, nil
	}

	for _, each := range samples {
		fmt.Printf("%s %s %d\n", each.Metric.String(), each.Value.String(), each.Timestamp)
	}
	return sensu.CheckStateOK, nil
}
