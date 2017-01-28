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
	"encoding/json"
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
	"github.com/spf13/pflag"

	k8s "github.com/YakLabs/k8s-client"
	k8s_http "github.com/YakLabs/k8s-client/http"
	api "github.com/pulcy/prometheus-conf-api"
	"github.com/pulcy/prometheus-conf/service"
	"github.com/pulcy/prometheus-conf/util"
)

var (
	maskAny = errgo.MaskFunc(errgo.Any)
)

const (
	logName           = "kubernetes"
	metricsAnnotation = "j2.pulcy.com/metrics"
	maxRecentErrors   = 30
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
	services         []k8s.Service
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
		return nil
	}
	p.client = c

	// Watch nodes for changes
	go func() {
		for {
			nodeEvents := make(chan k8s.NodeWatchEvent)
			go func() {
				for evt := range nodeEvents {
					if evt.Type() == k8s.WatchEventTypeAdded || evt.Type() == k8s.WatchEventTypeDeleted {
						p.log.Debugf("got node event of type %s", evt.Type())
						trigger <- fmt.Sprintf("node-%s", evt.Type())
					}
				}
			}()
			if err := p.client.WatchNodes(nil, nodeEvents); err != nil {
				p.log.Errorf("failed to watch nodes: %#v", err)
			}
		}
	}()

	// Watch services for changes
	go func() {
		for {
			serviceEvents := make(chan k8s.ServiceWatchEvent)
			go func() {
				for evt := range serviceEvents {
					p.log.Debugf("got service event of type %s", evt.Type())
					trigger <- fmt.Sprintf("service-%s", evt.Type())
				}
			}()
			if err := p.client.WatchServices("", nil, serviceEvents); err != nil {
				p.log.Errorf("failed to watch services: %#v", err)
			}
		}
	}()

	// No custom triggers here, just update once in a while.
	return nil
}

func (p *k8sPlugin) Update() (service.PluginUpdate, error) {
	if p.client == nil {
		return nil, nil
	}

	// Get nodes
	p.log.Debugf("fetching kubernetes nodes")
	nodes, nodesErr := p.client.ListNodes(nil)

	// Get services
	p.log.Debugf("fetching kubernetes services")
	services, servicesErr := p.client.ListServices("", nil)

	if nodesErr != nil || servicesErr != nil {
		if nodesErr != nil {
			p.log.Warningf("Failed to fetch kubernetes nodes: %#v (using previous ones)", nodesErr)
		}
		if servicesErr != nil {
			p.log.Warningf("Failed to fetch kubernetes services: %#v (using previous ones)", servicesErr)
		}
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
			services:         services.Items,
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
				Action: "labeldrop",
				Regex:  "etcd_debugging.*",
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
		JobName:        "kubernetes-nodes",
		ScrapeInterval: "5m",
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
	// Build URL list
	var urls []string
	for _, svc := range p.services {
		ann, ok := svc.Annotations[metricsAnnotation]
		if !ok || ann == "" {
			continue
		}

		var metricsRecords []api.MetricsServiceRecord
		if err := json.Unmarshal([]byte(ann), &metricsRecords); err != nil {
			p.log.Errorf("Failed to unmarshal metrics annotation in service '%s.%s': %#v", svc.Namespace, svc.Name, err)
			continue
		}

		// Get service IP
		if svc.Spec.Type != k8s.ServiceTypeClusterIP {
			p.log.Errorf("Cannot put metrics rules in services of type other than ClusterIP ('%s.%s')", svc.Namespace, svc.Name)
			continue
		}
		clusterIP := svc.Spec.ClusterIP

		// Collect URLs
		for _, m := range metricsRecords {
			if m.RulesPath == "" {
				continue
			}

			u := url.URL{
				Scheme: "http",
				Host:   net.JoinHostPort(clusterIP, strconv.Itoa(m.ServicePort)),
				Path:   m.RulesPath,
			}
			urls = append(urls, u.String())
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
