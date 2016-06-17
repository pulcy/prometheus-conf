// Copyright (c) 2016 Pulcy.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package fleet

import (
	"fmt"
	"net"
	"net/http"
	"net/url"

	"github.com/coreos/fleet/client"
	"github.com/juju/errgo"
	"github.com/op/go-logging"
	"github.com/spf13/pflag"

	"github.com/pulcy/prometheus-conf/service"
)

var (
	maskAny = errgo.MaskFunc(errgo.Any)
)

const (
	defaultNodeExporterPort = 9102
)

type fleetPlugin struct {
	log              *logging.Logger
	FleetURL         string
	NodeExporterPort int
}

func init() {
	service.RegisterPlugin("fleet", &fleetPlugin{
		log:              logging.MustGetLogger("fleet"),
		FleetURL:         "",
		NodeExporterPort: defaultNodeExporterPort,
	})
}

// Configure the command line flags needed by the plugin.
func (p *fleetPlugin) Setup(flagSet *pflag.FlagSet) {
	flagSet.StringVar(&p.FleetURL, "fleet-url", "", "URL of fleet")
	flagSet.IntVar(&p.NodeExporterPort, "node-exporter-port", defaultNodeExporterPort, "Port that node_exporters are listening on")
}

// Start the plugin. Send a value on the given channel to trigger an update of the configuration.
func (p *fleetPlugin) Start(trigger chan struct{}) {
	// No custom triggers here, just update once in a while.
}

// Extract data from fleet to create node_exporter targets
func (p *fleetPlugin) CreateNodes() ([]service.ScrapeConfig, error) {
	if p.FleetURL == "" {
		return nil, nil
	}

	// Connect to fleet
	url, err := url.Parse(p.FleetURL)
	if err != nil {
		return nil, maskAny(err)
	}
	httpClient, err := createHttpClient(url)
	if err != nil {
		return nil, maskAny(err)
	}
	fleet, err := client.NewHTTPClient(httpClient, *url)
	if err != nil {
		return nil, maskAny(err)
	}

	// Fetch machines
	p.log.Debugf("fetching fleet machines from %s", p.FleetURL)
	machines, err := fleet.Machines()
	if err != nil {
		return nil, maskAny(err)
	}

	// Build scrape config list
	tgNode := service.TargetGroup{}
	tgNode.Label("source", "node")
	tgEtcd := service.TargetGroup{}
	tgEtcd.Label("source", "etcd")
	for _, m := range machines {
		ip := m.PublicIP
		p.log.Debugf("found fleet machine %s", ip)
		tgNode.Targets = append(tgNode.Targets, fmt.Sprintf("%s:%d", ip, p.NodeExporterPort))
		tgEtcd.Targets = append(tgEtcd.Targets, fmt.Sprintf("%s:2379", ip))
	}

	scrapeConfig := service.ScrapeConfig{
		JobName:      "node",
		TargetGroups: []service.TargetGroup{tgNode, tgEtcd},
	}
	return []service.ScrapeConfig{scrapeConfig}, nil
}

func createHttpClient(url *url.URL) (*http.Client, error) {
	var trans http.RoundTripper

	switch url.Scheme {
	case "unix", "file":
		// The Path field is only used for dialing and should not be used when
		// building any further HTTP requests.
		sockPath := url.Path
		url.Path = ""

		// http.Client doesn't support the schemes "unix" or "file", but it
		// is safe to use "http" as dialFunc ignores it anyway.
		url.Scheme = "http"

		// The Host field is not used for dialing, but will be exposed in debug logs.
		url.Host = "domain-sock"

		trans = &http.Transport{
			Dial: func(s, t string) (net.Conn, error) {
				// http.Client does not natively support dialing a unix domain socket, so the
				// dial function must be overridden.
				return net.Dial("unix", sockPath)
			},
		}
	case "http", "https":
		trans = http.DefaultTransport
	default:
		return nil, maskAny(fmt.Errorf("Unknown scheme in fleet-url: %s", url.Scheme))
	}
	client := &http.Client{
		Transport: trans,
	}
	return client, nil
}
