// Copyright (c) 2017 Pulcy.
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

package kubernetes

import (
	"fmt"
	"os"

	"github.com/juju/errgo"
	"github.com/op/go-logging"
	"github.com/spf13/pflag"

	k8s "github.com/YakLabs/k8s-client"
	k8s_http "github.com/YakLabs/k8s-client/http"
	"github.com/pulcy/prometheus-conf/service"
	"github.com/pulcy/prometheus-conf/util"
)

var (
	maskAny = errgo.MaskFunc(errgo.Any)
)

const (
	logName         = "kubernetes"
	maxRecentErrors = 30
)

type k8sPlugin struct {
	LogLevel      string
	ETCDTLSConfig service.TLSConfig

	log              *logging.Logger
	client           k8s.Client
	lastUpdate       *k8sUpdate
	recentErrors     int
	nodeExporterPort int
}

type k8sUpdate struct {
	log              *logging.Logger
	nodeExporterPort int
	nodes            []k8s.Node
	ETCDTLSConfig    service.TLSConfig
}

func init() {
	service.RegisterPlugin("kubernetes", &k8sPlugin{
		log: logging.MustGetLogger(logName),
	})
}

// Configure the command line flags needed by the plugin.
func (p *k8sPlugin) Setup(flagSet *pflag.FlagSet) {
	flagSet.StringVar(&p.ETCDTLSConfig.CAFile, "kubernetes-etcd-ca-file", "", "CA certificate used by ETCD")
	flagSet.StringVar(&p.ETCDTLSConfig.CertFile, "kubernetes-etcd-cert-file", "", "Public key file used by ETCD")
	flagSet.StringVar(&p.ETCDTLSConfig.KeyFile, "kubernetes-etcd-key-file", "", "Private key file used by ETCD")
	flagSet.StringVar(&p.LogLevel, "kubernetes-log-level", "", "Log level of kubernetes plugin")
}

// Start the plugin. Send a value on the given channel to trigger an update of the configuration.
func (p *k8sPlugin) Start(config service.ServiceConfig, trigger chan string) error {
	if err := util.SetLogLevel(p.LogLevel, config.LogLevel, logName); err != nil {
		return maskAny(err)
	}
	// Setup kubernetes client
	p.nodeExporterPort = config.NodeExporterPort
	c, err := k8s_http.NewInCluster()
	if err != nil {
		p.log.Infof("No kubernetes available: %v", err)
	} else {
		p.client = c
	}

	// No custom triggers here, just update once in a while.
	return nil
}

func (p *k8sPlugin) Update() (service.PluginUpdate, error) {
	if p.client == nil {
		return nil, nil
	}

	// Get nodes
	p.log.Debugf("fetching kubernetes nodes")
	nodes, err := p.client.ListNodes(nil)
	if err != nil {
		p.log.Warningf("Failed to fetch kubernetes nodes: %#v (using previous ones)", err)
		p.recentErrors++
		if p.recentErrors > maxRecentErrors {
			p.log.Warningf("Too many recent kubernetes errors, restarting")
			os.Exit(1)
		}
		return p.lastUpdate, nil
	} else {
		p.recentErrors = 0
		update := &k8sUpdate{
			log:              p.log,
			nodeExporterPort: p.nodeExporterPort,
			nodes:            nodes.Items,
			ETCDTLSConfig:    p.ETCDTLSConfig,
		}
		p.lastUpdate = update
		return update, nil
	}
}

// Extract data from fleet to create node_exporter targets
func (p *k8sUpdate) CreateNodes() ([]service.ScrapeConfig, error) {
	// Build scrape config list
	scNode := service.StaticConfig{}
	scNode.Label("source", "node")
	scEtcd := service.StaticConfig{}
	scEtcd.Label("source", "etcd")
	for _, node := range p.nodes {
		for _, addr := range node.Status.Addresses {
			if addr.Type == "InternalIP" {
				ip := addr.Address
				p.log.Debugf("found kubernetes node %s", ip)
				scNode.Targets = append(scNode.Targets, fmt.Sprintf("%s:%d", ip, p.nodeExporterPort))
				if node.Labels["core"] == "true" {
					scEtcd.Targets = append(scEtcd.Targets, fmt.Sprintf("%s:2379", ip))
				}
			}
		}
	}

	scrapeConfigNode := service.ScrapeConfig{
		JobName:       "node",
		StaticConfigs: []service.StaticConfig{scNode},
	}
	scrapeConfigETCD := service.ScrapeConfig{
		JobName:       "etcd",
		StaticConfigs: []service.StaticConfig{scEtcd},
	}
	if p.ETCDTLSConfig.CAFile != "" && p.ETCDTLSConfig.CertFile != "" && p.ETCDTLSConfig.KeyFile != "" {
		scrapeConfigETCD.Scheme = "https"
		scrapeConfigETCD.TLSConfig = &service.TLSConfig{
			CAFile:             p.ETCDTLSConfig.CAFile,
			CertFile:           p.ETCDTLSConfig.CertFile,
			KeyFile:            p.ETCDTLSConfig.KeyFile,
			InsecureSkipVerify: true,
		}
	}
	scrapeConfigK8s := service.ScrapeConfig{
		JobName: "kubernetes",
		KubernetesConfigs: []service.KubernetesSDConfig{
			service.KubernetesSDConfig{
				Role: "node",
			},
			service.KubernetesSDConfig{
				Role: "service",
			},
			service.KubernetesSDConfig{
				Role: "pod",
			},
			service.KubernetesSDConfig{
				Role: "endpoints",
			},
		},
	}
	return []service.ScrapeConfig{scrapeConfigNode, scrapeConfigETCD, scrapeConfigK8s}, nil
}

// CreateRules creates all rules this plugin is aware of.
// The returns string list should contain the content of the various rules.
func (p *k8sUpdate) CreateRules() ([]string, error) {
	// No rules here
	return nil, nil
}
