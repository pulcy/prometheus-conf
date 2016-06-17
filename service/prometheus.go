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
	JobName      string          `yaml:"job_name"`
	HonorLabels  bool            `yaml:"honor_labels,omitempty"`
	MetricsPath  string          `yaml:"metrics_path,omitempty"`
	TargetGroups TargetGroupList `yaml:"target_groups,omitempty"`
}

type ScrapeConfigList []ScrapeConfig

type TargetGroup struct {
	Targets []string          `yaml:"targets,omitempty"`
	Labels  map[string]string `yaml:"labels,omitempty"`
}

type TargetGroupList []TargetGroup

func (pc *PrometheusConfig) Sort() {
	sort.Strings(pc.RuleFiles)
	for _, sc := range pc.ScrapeConfigs {
		sc.Sort()
	}
	sort.Sort(pc.ScrapeConfigs)
}

func (sc *ScrapeConfig) Sort() {
	for _, tg := range sc.TargetGroups {
		tg.Sort()
	}
	sort.Sort(sc.TargetGroups)
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

func (tg *TargetGroup) Label(name, value string) {
	if tg.Labels == nil {
		tg.Labels = make(map[string]string)
	}
	tg.Labels[name] = value
}

func (tg *TargetGroup) Sort() {
	sort.Strings(tg.Targets)
}

func (tg *TargetGroup) FullString() string {
	s := strings.Join(tg.Targets, ",")
	for k, v := range tg.Labels {
		s = s + k + v
	}
	return s
}

func (l TargetGroupList) Len() int {
	return len(l)
}

func (l TargetGroupList) Less(i, j int) bool {
	return l[i].FullString() < l[j].FullString()
}

func (l TargetGroupList) Swap(i, j int) {
	l[i], l[j] = l[j], l[i]
}
