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

type PrometheusConfig struct {
	Global        GlobalConfig   `yaml:"global"`
	RuleFiles     []string       `yaml:"rule_files,omitempty"`
	ScrapeConfigs []ScrapeConfig `yaml:"scrape_configs"`
}

type GlobalConfig struct {
	ScrapeInterval     string `yaml:"scrape_interval,omitempty"`
	EvaluationInterval string `yaml:"evaluation_interval,omitempty"`
}

type ScrapeConfig struct {
	JobName      string        `yaml:"job_name"`
	HonorLabels  bool          `yaml:"honor_labels,omitempty"`
	MetricsPath  string        `yaml:"metrics_path,omitempty"`
	TargetGroups []TargetGroup `yaml:"target_groups,omitempty"`
}

type TargetGroup struct {
	Targets []string          `yaml:"targets,omitempty"`
	Labels  map[string]string `yaml:"labels,omitempty"`
}

func (tg *TargetGroup) Label(name, value string) {
	if tg.Labels == nil {
		tg.Labels = make(map[string]string)
	}
	tg.Labels[name] = value
}
