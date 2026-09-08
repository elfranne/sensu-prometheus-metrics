[![Sensu Bonsai Asset](https://img.shields.io/badge/Bonsai-Download%20Me-brightgreen.svg?colorB=89C967&logo=sensu)](https://bonsai.sensu.io/assets/elfranne/sensu-prometheus-metrics)
![Go Test](https://github.com/elfranne/sensu-prometheus-metrics/workflows/Go%20Test/badge.svg)
![goreleaser](https://github.com/elfranne/sensu-prometheus-metrics/workflows/goreleaser/badge.svg)

# sensu-prometheus-metrics

## Table of Contents
- [Overview](#overview)
- [Output](#output)
- [Usage examples](#usage-examples)
  - [Help](#help)
  - [Adding labels](#adding-labels)
  - [Basic auth](#basic-auth)
  - [TLS and mTLS](#tls-and-mtls)
- [Configuration](#configuration)
  - [Asset registration](#asset-registration)
  - [Check definition](#check-definition)
  - [Annotations](#annotations)
- [Installation from source](#installation-from-source)
- [Contributing](#contributing)

## Overview

sensu-prometheus-metrics is a [Sensu Check][6] that scrapes a Prometheus
exporter and prints the samples it finds in Sensu's `prometheus_text` metric
format, so any Prometheus endpoint can be collected by a Sensu agent and routed
to a metric handler.

It scrapes a single endpoint over HTTP or HTTPS, optionally authenticating with
basic auth or a client certificate, and can decorate every sample it emits with
extra labels supplied on the command line.

The check exits `0` (OK) once the endpoint has been scraped and parsed. If the
endpoint cannot be reached, answers with a non-`200` status, or returns a body
that is not valid Prometheus exposition text, the check prints the reason and
exits `3` (UNKNOWN); no metrics are emitted in that case.

## Output

Each sample is printed on its own line as:

```
<metric_name>{<label>="<value>", ...} <value> <timestamp>
```

Metrics without labels are printed without the braces.

Timestamps are Unix **milliseconds**, which is what Sensu's `prometheus_text`
parser expects. Samples that carry their own timestamp in the exposition text
keep it; every other sample is stamped with the time of the scrape.

```
go_goroutines 42 1727452800123
http_requests_total{code="200", method="get"} 1027 1727452800123
http_requests_total{code="500", method="get"} 3 1727452800123
```

## Usage examples

### Help

```
Check metrics from Prometheus

Usage:
  sensu-prometheus-metrics [flags]
  sensu-prometheus-metrics [command]

Available Commands:
  completion  Generate the autocompletion script for the specified shell
  help        Help about any command
  version     Print the version number of this plugin

Flags:
      --cacert string        CA cert to use for mTLS
      --cert string          Cert to use for mTLS
  -h, --help                 help for sensu-prometheus-metrics
      --insecureskipverify   insecureskipverify option if using self signed certs.
      --key string           Key to use for mTLS
      --label strings        labels to add to metrics
      --password string      Password for basic auth
      --url string           URL to the Prometheus metrics (default "http://localhost:8405/metrics")
      --user string          User for basic auth

Use "sensu-prometheus-metrics [command] --help" for more information about a command.
```

Scrape a local exporter:

```
sensu-prometheus-metrics --url http://localhost:9100/metrics
```

### Adding labels

`--label` takes a `name:value` pair and may be repeated. Every sample returned
by the exporter gets every label, and a label given here overwrites a label of
the same name coming from the exporter. Whitespace around the name and the
value is trimmed, and only the first colon is treated as the separator, so
values may themselves contain colons.

```
sensu-prometheus-metrics \
  --url http://localhost:9100/metrics \
  --label env:production \
  --label "source:http://localhost:9100"
```

### Basic auth

Both `--user` and `--password` must be set; the `Authorization` header is only
sent when neither is empty.

```
sensu-prometheus-metrics --url https://exporter.example.com/metrics --user sensu --password s3cret
```

### TLS and mTLS

For a server certificate that does not chain to a trusted root — a self-signed
exporter, for instance — verification can be turned off:

```
sensu-prometheus-metrics --url https://exporter.example.com/metrics --insecureskipverify
```

For mutual TLS, pass the client certificate, its key, and the CA that signed the
exporter's certificate. All three belong together: setting any one of `--cert`,
`--key` or `--cacert` makes the check load all three, and it fails if one is
missing or unreadable. When they are used, the server certificate is verified
against `--cacert` and `--insecureskipverify` has no effect.

```
sensu-prometheus-metrics \
  --url https://exporter.example.com/metrics \
  --cert /etc/sensu/certs/client.pem \
  --key /etc/sensu/certs/client-key.pem \
  --cacert /etc/sensu/certs/ca.pem
```

## Configuration

### Asset registration

[Sensu Assets][10] are the best way to make use of this plugin. If you're not using an asset, please
consider doing so! If you're using sensuctl 5.13 with Sensu Backend 5.13 or later, you can use the
following command to add the asset:

```
sensuctl asset add elfranne/sensu-prometheus-metrics
```

If you're using an earlier version of sensuctl, you can find the asset on the
[Bonsai Asset Index](https://bonsai.sensu.io/assets/elfranne/sensu-prometheus-metrics).

### Check definition

The check writes metrics to stdout, so the check must declare
`output_metric_format: prometheus_text` and the handlers that should receive
them.

```yml
---
type: CheckConfig
api_version: core/v2
metadata:
  name: sensu-prometheus-metrics
  namespace: default
spec:
  command: sensu-prometheus-metrics --url http://localhost:9100/metrics --label env:production
  subscriptions:
  - system
  interval: 60
  publish: true
  output_metric_format: prometheus_text
  output_metric_handlers:
  - influxdb
  runtime_assets:
  - elfranne/sensu-prometheus-metrics
```

### Annotations

Every flag can also be set per entity or per check through annotations under the
plugin's keyspace, `sensu.io/plugins/sensu-prometheus-metrics/config`. This is
the usual way to keep credentials out of the check command:

```yml
---
type: Entity
api_version: core/v2
metadata:
  name: exporter-host
  namespace: default
  annotations:
    sensu.io/plugins/sensu-prometheus-metrics/config/url: http://localhost:9100/metrics
    sensu.io/plugins/sensu-prometheus-metrics/config/user: sensu
    sensu.io/plugins/sensu-prometheus-metrics/config/password: s3cret
```

## Installation from source

The preferred way of installing and deploying this plugin is to use it as an Asset. If you would
like to compile and install the plugin from source or contribute to it, download the latest version
or create an executable script from this source.

From the local path of the sensu-prometheus-metrics repository:

```
go build
```

And to run the tests:

```
go test ./...
```

## Contributing

For more information about contributing to this plugin, see [Contributing][1].

[1]: https://github.com/sensu/sensu-go/blob/master/CONTRIBUTING.md
[6]: https://docs.sensu.io/sensu-go/latest/reference/checks/
[10]: https://docs.sensu.io/sensu-go/latest/reference/assets/
