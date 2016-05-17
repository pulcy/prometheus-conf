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

package service

import (
	"fmt"
	"net"
	"net/http"
	"net/url"

	"github.com/coreos/fleet/client"
)

// Extract data from fleet to create node_exporter targets
func (s *Service) createFleetNodes() ([]ScrapeConfig, error) {
	if s.FleetURL == "" {
		return nil, nil
	}

	// Connect to fleet
	url, err := url.Parse(s.FleetURL)
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
	s.Log.Debugf("fetching fleet machines from %s", s.FleetURL)
	machines, err := fleet.Machines()
	if err != nil {
		return nil, maskAny(err)
	}

	// Build scrape config list
	tgNode := TargetGroup{}
	tgNode.Label("source", "node")
	tgEtcd := TargetGroup{}
	tgEtcd.Label("source", "etcd")
	for _, m := range machines {
		ip := m.PublicIP
		s.Log.Debugf("found fleet machine %s", ip)
		tgNode.Targets = append(tgNode.Targets, fmt.Sprintf("%s:%d", ip, s.NodeExporterPort))
		tgEtcd.Targets = append(tgEtcd.Targets, fmt.Sprintf("%s:2379", ip))
	}

	scrapeConfig := ScrapeConfig{
		JobName:      "node",
		TargetGroups: []TargetGroup{tgNode, tgEtcd},
	}
	return []ScrapeConfig{scrapeConfig}, nil
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
