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
	"os"

	"github.com/coreos/fleet/client"
	"github.com/coreos/fleet/machine"
	"github.com/juju/errgo"
	"github.com/op/go-logging"
	"github.com/spf13/pflag"

	"github.com/pulcy/prometheus-conf/service"
	"github.com/pulcy/prometheus-conf/util"
)

var (
	maskAny = errgo.MaskFunc(errgo.Any)
)

const (
	logName         = "fleet"
	maxRecentErrors = 30
)

type fleetPlugin struct {
	FleetURL      string
	FleetLogLevel string

	log              *logging.Logger
	lastUpdate       *fleetUpdate
	recentErrors     int
	nodeExporterPort int
}

type fleetUpdate struct {
	log              *logging.Logger
	nodeExporterPort int
	machines         []machine.MachineState
}

func init() {
	service.RegisterPlugin("fleet", &fleetPlugin{
		log:      logging.MustGetLogger(logName),
		FleetURL: "",
	})
}

// Configure the command line flags needed by the plugin.
func (p *fleetPlugin) Setup(flagSet *pflag.FlagSet) {
	flagSet.StringVar(&p.FleetURL, "fleet-url", "", "URL of fleet")
	flagSet.StringVar(&p.FleetLogLevel, "fleet-log-level", "", "Log level of fleet plugin")
}

// Start the plugin. Send a value on the given channel to trigger an update of the configuration.
func (p *fleetPlugin) Start(config service.ServiceConfig, trigger chan string) error {
	if err := util.SetLogLevel(p.FleetLogLevel, config.LogLevel, logName); err != nil {
		return maskAny(err)
	}
	p.nodeExporterPort = config.NodeExporterPort
	// No custom triggers here, just update once in a while.
	return nil
}

func (p *fleetPlugin) Update() (service.PluginUpdate, error) {
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
	if tr, ok := httpClient.Transport.(*http.Transport); ok {
		defer tr.CloseIdleConnections()
	}
	fleet, err := client.NewHTTPClient(httpClient, *url)
	if err != nil {
		return nil, maskAny(err)
	}

	// Fetch machines
	p.log.Debugf("fetching fleet machines from %s", p.FleetURL)
	machines, err := fleet.Machines()
	if err != nil {
		p.log.Warningf("Failed to fetch machines from %s: %#v (using previous ones)", p.FleetURL, err)
		p.recentErrors++
		if p.recentErrors > maxRecentErrors {
			p.log.Warningf("Too many recent fleet errors, restarting")
			os.Exit(1)
		}
		return p.lastUpdate, nil
	} else {
		p.recentErrors = 0
		update := &fleetUpdate{
			log:              p.log,
			nodeExporterPort: p.nodeExporterPort,
			machines:         machines,
		}
		p.lastUpdate = update
		return update, nil
	}
}

// Extract data from fleet to create node_exporter targets
func (p *fleetUpdate) CreateNodes() ([]service.ScrapeConfig, error) {
	// Build scrape config list
	scNode := service.StaticConfig{}
	scNode.Label("source", "node")
	scEtcd := service.StaticConfig{}
	scEtcd.Label("source", "etcd")
	scFleet := service.StaticConfig{}
	scFleet.Label("source", "fleet")
	for _, m := range p.machines {
		ip := m.PublicIP
		p.log.Debugf("found fleet machine %s", ip)
		scNode.Targets = append(scNode.Targets, fmt.Sprintf("%s:%d", ip, p.nodeExporterPort))
		scEtcd.Targets = append(scEtcd.Targets, fmt.Sprintf("%s:2379", ip))
		scFleet.Targets = append(scFleet.Targets, fmt.Sprintf("%s:49153", ip))
	}

	scrapeConfigNode := service.ScrapeConfig{
		JobName:       "node",
		StaticConfigs: []service.StaticConfig{scNode},
	}
	scrapeConfigETCD := service.ScrapeConfig{
		JobName:       "etcd",
		StaticConfigs: []service.StaticConfig{scEtcd},
	}
	scrapeConfigFleet := service.ScrapeConfig{
		JobName:       "fleet",
		StaticConfigs: []service.StaticConfig{scFleet},
	}
	return []service.ScrapeConfig{scrapeConfigNode, scrapeConfigETCD, scrapeConfigFleet}, nil
}

// CreateRules creates all rules this plugin is aware of.
// The returns string list should contain the content of the various rules.
func (p *fleetUpdate) CreateRules() ([]string, error) {
	// No rules here
	return nil, nil
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
			DisableKeepAlives: true,
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
