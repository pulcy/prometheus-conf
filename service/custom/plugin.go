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

package custom

import (
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"

	"github.com/juju/errgo"
	"github.com/op/go-logging"
	"github.com/pulcy/prometheus-conf-api"
	regapi "github.com/pulcy/registrator-api"
	"github.com/spf13/pflag"

	"github.com/pulcy/prometheus-conf/service"
	"github.com/pulcy/prometheus-conf/util"
)

var (
	maskAny = errgo.MaskFunc(errgo.Any)
)

const (
	logName         = "custom"
	defaultEtcdPath = "/pulcy/metrics"
	defaultEtcdURL  = "http://127.0.0.1:2379" + defaultEtcdPath
)

type customPlugin struct {
	log            *logging.Logger
	EtcdURL        string
	CustomLogLevel string

	backend     *etcdBackend
	registrator regapi.API
	rulesCache  map[string]string
}

type customUpdate struct {
	log      *logging.Logger
	metrics  []api.MetricsServiceRecord
	services []regapi.Service
}

func init() {
	service.RegisterPlugin("custom", &customPlugin{
		log:        logging.MustGetLogger(logName),
		EtcdURL:    defaultEtcdURL,
		rulesCache: make(map[string]string),
	})
}

// Configure the command line flags needed by the plugin.
func (p *customPlugin) Setup(flagSet *pflag.FlagSet) {
	flagSet.StringVar(&p.EtcdURL, "etcd-url", defaultEtcdURL, "URL of ETCD")
	flagSet.StringVar(&p.CustomLogLevel, "custom-log-level", "", "Log level of the custom plugin")
}

// Start the plugin. Send a value on the given channel to trigger an update of the configuration.
func (p *customPlugin) Start(config service.ServiceConfig, trigger chan string) error {
	if err := util.SetLogLevel(p.CustomLogLevel, config.LogLevel, logName); err != nil {
		return maskAny(err)
	}
	if p.EtcdURL == "" {
		return nil
	}
	etcdClient, etcdPath, err := newEtcdClient(p.EtcdURL)
	if err != nil {
		return maskAny(err)
	}
	p.backend, err = newEtcdBackend(etcdClient, etcdPath, p.log)
	if err != nil {
		return maskAny(err)
	}
	p.registrator, err = regapi.NewRegistratorClient(etcdClient, "", p.log)
	if err != nil {
		return maskAny(err)
	}
	// Ensure rules dir exists
	if err := os.MkdirAll(config.RulesPath, 0755); err != nil {
		return maskAny(err)
	}

	// Watch for backend updates
	go func() {
		for {
			if err := p.backend.Watch(); err != nil {
				p.log.Errorf("backend watch failed: %#v", err)
			}
			trigger <- "custom-backend"
		}
	}()

	// Watch for registrator updates
	go func() {
		for {
			if err := p.registrator.Watch(); err != nil {
				p.log.Errorf("registrator watch failed: %#v", err)
			}
			trigger <- "custom-registrator"
		}
	}()

	return nil
}

func (p *customPlugin) Update() (service.PluginUpdate, error) {
	if p.backend == nil || p.registrator == nil {
		return nil, nil
	}

	metrics, err := p.backend.Metrics()
	if err != nil {
		return nil, maskAny(err)
	}
	services, err := p.registrator.Services()
	if err != nil {
		return nil, maskAny(err)
	}

	return &customUpdate{
		log:      p.log,
		metrics:  metrics,
		services: services,
	}, nil
}

// Extract data from the backend and registrator to create configured metrics targets
func (p *customUpdate) CreateNodes() ([]service.ScrapeConfig, error) {
	var result []service.ScrapeConfig
	for _, m := range p.metrics {
		scrapeConfig := service.ScrapeConfig{
			JobName:     m.ServiceName,
			MetricsPath: m.MetricsPath,
		}

		for _, s := range p.services {
			if !isMatch(m, s) {
				continue
			}

			// Build scrape config list
			sc := service.StaticConfig{}
			sc.Label("source", fmt.Sprintf("%s-%d", m.ServiceName, m.ServicePort))
			for _, instance := range s.Instances {
				sc.Targets = append(sc.Targets, fmt.Sprintf("%s:%d", instance.IP, instance.Port))
			}

			// Add target group
			scrapeConfig.StaticConfigs = append(scrapeConfig.StaticConfigs, sc)
		}

		if len(scrapeConfig.StaticConfigs) > 0 {
			result = append(result, scrapeConfig)
		} else {
			p.log.Debugf("No services found for metrics %s-%d", m.ServiceName, m.ServicePort)
		}
	}

	return result, nil
}

// CreateRules creates all rules this plugin is aware of.
// The returns string list should contain the content of the various rules.
func (p *customUpdate) CreateRules() ([]string, error) {
	// Build URL list
	var urls []string
	for _, m := range p.metrics {
		if m.RulesPath == "" {
			continue
		}

		for _, s := range p.services {
			if !isMatch(m, s) {
				continue
			}

			// Build URL for every instance
			for _, instance := range s.Instances {
				u := url.URL{
					Scheme: "http",
					Host:   net.JoinHostPort(instance.IP, strconv.Itoa(instance.Port)),
					Path:   m.RulesPath,
				}
				urls = append(urls, u.String())
			}
		}
	}

	if len(urls) == 0 {
		return nil, nil
	}

	// Fetch rules from URLs
	rulesChan := make(chan string, len(urls))
	wg := sync.WaitGroup{}
	for _, url := range urls {
		wg.Add(1)
		go func(url string) {
			defer wg.Done()
			p.log.Debugf("fetching rules from %s", url)
			resp, err := http.Get(url)
			if err != nil {
				p.log.Errorf("Failed to fetch rule from '%s': %#v", url, err)
				return
			}
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				p.log.Errorf("Failed to fetch rule from '%s': Status %d", url, resp.StatusCode)
				return
			}
			defer resp.Body.Close()
			raw, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				p.log.Errorf("Failed to read rule from '%s': %#v", url, err)
				return
			}
			rulesChan <- string(raw)
			p.log.Debugf("done fetching rules from %s", url)
		}(url)
	}

	wg.Wait()
	close(rulesChan)
	var result []string
	for rule := range rulesChan {
		result = append(result, rule)
	}
	p.log.Debugf("Found %d rules", len(result))

	return result, nil
}

func isMatch(m api.MetricsServiceRecord, s regapi.Service) bool {
	if m.ServiceName != s.ServiceName {
		return false
	}
	if m.ServicePort != 0 && m.ServicePort != s.ServicePort {
		return false
	}
	return true
}
