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

	"github.com/juju/errgo"
	"github.com/op/go-logging"
	regapi "github.com/pulcy/registrator-api"
	"github.com/spf13/pflag"

	"github.com/pulcy/prometheus-conf/service"
)

var (
	maskAny = errgo.MaskFunc(errgo.Any)
)

const (
	defaultEtcdPath = "/pulcy/metrics"
	defaultEtcdURL  = "http://127.0.0.1:2379" + defaultEtcdPath
)

type customPlugin struct {
	log     *logging.Logger
	EtcdURL string

	backend     *etcdBackend
	registrator regapi.API
}

func init() {
	service.RegisterPlugin("custom", &customPlugin{
		log:     logging.MustGetLogger("custom"),
		EtcdURL: defaultEtcdURL,
	})
}

// Configure the command line flags needed by the plugin.
func (p *customPlugin) Setup(flagSet *pflag.FlagSet) {
	flagSet.StringVar(&p.EtcdURL, "etcd-url", defaultEtcdURL, "URL of ETCD")
}

// Start the plugin. Send a value on the given channel to trigger an update of the configuration.
func (p *customPlugin) Start(trigger chan struct{}) error {
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

	// Watch for backend updates
	go func() {
		for {
			if err := p.backend.Watch(); err != nil {
				p.log.Errorf("backend watch failed: %#v", err)
			}
			trigger <- struct{}{}
		}
	}()

	// Watch for registrator updates
	go func() {
		for {
			if err := p.registrator.Watch(); err != nil {
				p.log.Errorf("registrator watch failed: %#v", err)
			}
			trigger <- struct{}{}
		}
	}()

	return nil
}

// Extract data from the backend and registrator to create configured metrics targets
func (p *customPlugin) CreateNodes() ([]service.ScrapeConfig, error) {
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

	var result []service.ScrapeConfig
	for _, m := range metrics {
		scrapeConfig := service.ScrapeConfig{
			JobName:     m.ServiceName,
			MetricsPath: m.MetricsPath,
		}

		for _, s := range services {
			if m.ServiceName != s.ServiceName || m.ServicePort != s.ServicePort {
				continue
			}

			// Build scrape config list
			tg := service.TargetGroup{}
			tg.Label("source", fmt.Sprintf("%s-%d", m.ServiceName, m.ServicePort))
			for _, instance := range s.Instances {
				tg.Targets = append(tg.Targets, fmt.Sprintf("%s:%d", instance.IP, instance.Port))
			}

			// Add target group
			scrapeConfig.TargetGroups = append(scrapeConfig.TargetGroups, tg)
		}

		if len(scrapeConfig.TargetGroups) > 0 {
			result = append(result, scrapeConfig)
		}
	}

	return result, nil
}
