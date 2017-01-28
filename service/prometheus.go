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
	"sort"
	"strings"
)

type PrometheusConfig struct {
	Global        GlobalConfig     `yaml:"global"`
	RuleFiles     []string         `yaml:"rule_files,omitempty"`
	ScrapeConfigs ScrapeConfigList `yaml:"scrape_configs"`
}

type GlobalConfig struct {
	ScrapeInterval     string `yaml:"scrape_interval,omitempty"`
	EvaluationInterval string `yaml:"evaluation_interval,omitempty"`
}

type ScrapeConfig struct {
	JobName           string                 `yaml:"job_name"`
	ScrapeInterval    string                 `yaml:"scrape_interval,omitempty"`
	HonorLabels       bool                   `yaml:"honor_labels,omitempty"`
	MetricsPath       string                 `yaml:"metrics_path,omitempty"`
	Scheme            string                 `yaml:"scheme,omitempty"`
	StaticConfigs     StaticConfigList       `yaml:"static_configs,omitempty"`
	KubernetesConfigs KubernetesSDConfigList `yaml:"kubernetes_sd_configs,omitempty"`
	TLSConfig         *TLSConfig             `yaml:"tls_config,omitempty"`
	RelabelConfigs    RelabelConfigList      `yaml:"relabel_configs,omitempty"`
}

type ScrapeConfigList []ScrapeConfig

type KubernetesSDConfig struct {
	Role string `yaml:"role"`
}

type KubernetesSDConfigList []KubernetesSDConfig

type StaticConfig struct {
	Targets []string          `yaml:"targets,omitempty"`
	Labels  map[string]string `yaml:"labels,omitempty"`
}

type StaticConfigList []StaticConfig

type TLSConfig struct {
	CAFile             string `yaml:"ca_file,omitempty"`
	CertFile           string `yaml:"cert_file,omitempty"`
	KeyFile            string `yaml:"key_file,omitempty"`
	ServerName         string `yaml:"server_name,omitempty"`
	InsecureSkipVerify bool   `yaml:"insecure_skip_verify,omitempty"`
}

// IsConfigured returns true if there is a non-empty configuration in the given TLSConfig.
func (t TLSConfig) IsConfigured() bool {
	if t.CAFile != "" && t.CertFile != "" && t.KeyFile != "" {
		return true
	}
	return false
}

type RelabelConfig struct {
	SourceLabels []string `yaml:"source_labels,omitempty"`
	Separator    string   `yaml:"separator,omitempty"`
	TargetLabel  string   `yaml:"target_label,omitempty"`
	Regex        string   `yaml:"regex,omitempty"`
	Modulus      uint64   `yaml:"modulus,omitempty"`
	Replacement  string   `yaml:"replacement,omitempty"`
	Action       string   `yaml:"action,omitempty"`
}

type RelabelConfigList []RelabelConfig

func (pc *PrometheusConfig) Sort() {
	sort.Strings(pc.RuleFiles)
	for _, sc := range pc.ScrapeConfigs {
		sc.Sort()
	}
	sort.Sort(pc.ScrapeConfigs)
}

func (sc *ScrapeConfig) Sort() {
	for _, c := range sc.StaticConfigs {
		c.Sort()
	}
	sort.Sort(sc.StaticConfigs)
}

func (l ScrapeConfigList) Len() int {
	return len(l)
}

func (l ScrapeConfigList) Less(i, j int) bool {
	return l[i].JobName < l[j].JobName
}

func (l ScrapeConfigList) Swap(i, j int) {
	l[i], l[j] = l[j], l[i]
}

func (tg *StaticConfig) Label(name, value string) {
	if tg.Labels == nil {
		tg.Labels = make(map[string]string)
	}
	tg.Labels[name] = value
}

func (tg *StaticConfig) Sort() {
	sort.Strings(tg.Targets)
}

func (tg *StaticConfig) FullString() string {
	s := strings.Join(tg.Targets, ",")
	for k, v := range tg.Labels {
		s = s + k + v
	}
	return s
}

func (l StaticConfigList) Len() int {
	return len(l)
}

func (l StaticConfigList) Less(i, j int) bool {
	return l[i].FullString() < l[j].FullString()
}

func (l StaticConfigList) Swap(i, j int) {
	l[i], l[j] = l[j], l[i]
}
