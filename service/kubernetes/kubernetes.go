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
	LogLevel         string
	ETCDTLSConfig    service.TLSConfig
	KubeletTLSConfig service.TLSConfig

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
	etcdTLSConfig    service.TLSConfig
	kubeletTLSConfig service.TLSConfig
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
	flagSet.StringVar(&p.KubeletTLSConfig.CAFile, "kubelet-ca-file", "", "CA certificate used by Kubelet")
	flagSet.StringVar(&p.KubeletTLSConfig.CertFile, "kubelet-cert-file", "", "Public key file used by Kubelet")
	flagSet.StringVar(&p.KubeletTLSConfig.KeyFile, "kubelet-key-file", "", "Private key file used by Kubelet")
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
			etcdTLSConfig:    p.ETCDTLSConfig,
			kubeletTLSConfig: p.KubeletTLSConfig,
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
		RelabelConfigs: []service.RelabelConfig{
			service.RelabelConfig{
				SourceLabels: []string{"__address__"},
				Action:       "replace",
				TargetLabel:  "instance",
				Regex:        `(.+)(?::)(\d+)`,
				Replacement:  "$1",
			},
			service.RelabelConfig{
				SourceLabels: []string{"__address__"},
				Action:       "replace",
				TargetLabel:  "port",
				Regex:        `(.+)(?::)(\d+)`,
				Replacement:  "$2",
			},
		},
	}
	scrapeConfigETCD := service.ScrapeConfig{
		JobName:       "etcd",
		StaticConfigs: []service.StaticConfig{scEtcd},
		RelabelConfigs: []service.RelabelConfig{
			service.RelabelConfig{
				SourceLabels: []string{"__address__"},
				Action:       "replace",
				TargetLabel:  "instance",
				Regex:        `(.+)(?::)(\d+)`,
				Replacement:  "$1",
			},
			service.RelabelConfig{
				SourceLabels: []string{"__address__"},
				Action:       "replace",
				TargetLabel:  "port",
				Regex:        `(.+)(?::)(\d+)`,
				Replacement:  "$2",
			},
		},
	}
	if p.etcdTLSConfig.IsConfigured() {
		scrapeConfigETCD.Scheme = "https"
		scrapeConfigETCD.TLSConfig = &service.TLSConfig{
			CAFile:             p.etcdTLSConfig.CAFile,
			CertFile:           p.etcdTLSConfig.CertFile,
			KeyFile:            p.etcdTLSConfig.KeyFile,
			InsecureSkipVerify: true,
		}
	}
	scrapeConfigK8sNodes := service.ScrapeConfig{
		JobName: "kubernetes-nodes",
		KubernetesConfigs: []service.KubernetesSDConfig{
			service.KubernetesSDConfig{
				Role: "node",
			},
		},
		RelabelConfigs: []service.RelabelConfig{
			service.RelabelConfig{
				Action: "labelmap",
				Regex:  "__meta_kubernetes_node_label_(.+)",
			},
		},
	}
	if p.kubeletTLSConfig.IsConfigured() {
		scrapeConfigK8sNodes.Scheme = "https"
		scrapeConfigK8sNodes.TLSConfig = &service.TLSConfig{
			CAFile:             p.kubeletTLSConfig.CAFile,
			CertFile:           p.kubeletTLSConfig.CertFile,
			KeyFile:            p.kubeletTLSConfig.KeyFile,
			InsecureSkipVerify: true,
		}
	}
	scrapeConfigK8sEndpoinds := service.ScrapeConfig{
		JobName: "kubernetes-endpoints",
		KubernetesConfigs: []service.KubernetesSDConfig{
			service.KubernetesSDConfig{
				Role: "endpoints",
			},
		},
		RelabelConfigs: []service.RelabelConfig{
			service.RelabelConfig{
				SourceLabels: []string{"__meta_kubernetes_service_annotation_prometheus_io_scrape"},
				Action:       "keep",
				Regex:        "true",
			},
			service.RelabelConfig{
				SourceLabels: []string{"__meta_kubernetes_service_annotation_prometheus_io_scheme"},
				Action:       "replace",
				TargetLabel:  "__scheme__",
				Regex:        "(https?)",
			},
			service.RelabelConfig{
				SourceLabels: []string{"__meta_kubernetes_service_annotation_prometheus_io_path"},
				Action:       "replace",
				TargetLabel:  "__metrics_path__",
				Regex:        "(.+)",
			},
			service.RelabelConfig{
				SourceLabels: []string{"__address__", "__meta_kubernetes_service_annotation_prometheus_io_port"},
				Action:       "replace",
				TargetLabel:  "__address__",
				Regex:        `(.+)(?::\d+);(\d+)`,
				Replacement:  "$1:$2",
			},
			service.RelabelConfig{
				Action: "labelmap",
				Regex:  "__meta_kubernetes_service_label_(.+)",
			},
			service.RelabelConfig{
				SourceLabels: []string{"__meta_kubernetes_namespace"},
				Action:       "replace",
				TargetLabel:  "kubernetes_namespace",
			},
			service.RelabelConfig{
				SourceLabels: []string{"__meta_kubernetes_pod_name"},
				Action:       "replace",
				TargetLabel:  "kubernetes_pod_name",
			},
			service.RelabelConfig{
				SourceLabels: []string{"__address__"},
				Action:       "replace",
				TargetLabel:  "instance",
				Regex:        `(.+)(?::)(\d+)`,
				Replacement:  "$1",
			},
			service.RelabelConfig{
				SourceLabels: []string{"__address__"},
				Action:       "replace",
				TargetLabel:  "port",
				Regex:        `(.+)(?::)(\d+)`,
				Replacement:  "$2",
			},
			service.RelabelConfig{
				SourceLabels: []string{"j2_job_name"},
				Action:       "replace",
				TargetLabel:  "job",
			},
			service.RelabelConfig{
				SourceLabels: []string{"j2_taskgroup_name"},
				Action:       "replace",
				TargetLabel:  "taskgroup",
			},
		},
	}
	return []service.ScrapeConfig{scrapeConfigNode, scrapeConfigETCD, scrapeConfigK8sNodes, scrapeConfigK8sEndpoinds}, nil
}

// CreateRules creates all rules this plugin is aware of.
// The returns string list should contain the content of the various rules.
func (p *k8sUpdate) CreateRules() ([]string, error) {
	// No rules here
	return nil, nil
}
