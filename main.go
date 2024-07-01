package main

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
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
	Url    string
	Labels []string
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
	}
)

func main() {
	check := sensu.NewCheck(&plugin.PluginConfig, options, checkArgs, executeCheck, false)
	check.Execute()
}

func checkArgs(event *corev2.Event) (int, error) {
	return sensu.CheckStateOK, nil
}
func QueryExporter(exporterURL string, Labels []string) (model.Vector, error) {

	tlsconfig := &tls.Config{InsecureSkipVerify: true}
	tr := &http.Transport{
		TLSClientConfig: tlsconfig,
	}
	client := &http.Client{Transport: tr}
	req, err := http.NewRequest("GET", exporterURL, nil)
	if err != nil {
		return nil, err
	}

	expResponse, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer expResponse.Body.Close()

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
		Timestamp: model.Time(time.Now().Unix()),
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

	samples, err = QueryExporter(plugin.Url, plugin.Labels)
	if err != nil {
		fmt.Printf("Failed: %s\n", err)
		return sensu.CheckStateUnknown, nil
	}

	for _, each := range samples {
		fmt.Printf("%s %s %d\n", each.Metric.String(), each.Value.String(), each.Timestamp)
	}
	return sensu.CheckStateOK, nil
}
