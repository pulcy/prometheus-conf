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
	"io/ioutil"
	"time"

	"github.com/go-yaml/yaml"
	"github.com/op/go-logging"
)

type ServiceConfig struct {
	ConfigPath string
	Once       bool
	LoopDelay  time.Duration

	FleetURL         string
	NodeExporterPort int
}

type ServiceDependencies struct {
	Log *logging.Logger
}
type Service struct {
	ServiceConfig
	ServiceDependencies
}

func NewService(config ServiceConfig, deps ServiceDependencies) *Service {
	return &Service{
		ServiceConfig:       config,
		ServiceDependencies: deps,
	}
}

// Build the config once of continuously
func (s *Service) Run() error {
	for {
		err := s.runOnce()
		if err != nil {
			s.Log.Errorf("runOnce failed: %#v", err)
		}
		if s.Once {
			return maskAny(err)
		}
		time.Sleep(s.LoopDelay)
	}
	return nil
}

func (s *Service) runOnce() error {
	// Build configuration object
	config, err := s.createConfig()
	if err != nil {
		return maskAny(err)
	}

	// Save config
	raw, err := yaml.Marshal(config)
	if err != nil {
		return maskAny(err)
	}
	if err := ioutil.WriteFile(s.ConfigPath, raw, 0755); err != nil {
		return maskAny(err)
	}

	return nil
}

// createConfig builds he configuration file (in memory)
func (s *Service) createConfig() (PrometheusConfig, error) {
	config := PrometheusConfig{}

	// Fleet
	cfgs, err := s.createFleetNodes()
	if err != nil {
		return config, maskAny(err)
	}
	config.ScrapeConfigs = append(config.ScrapeConfigs, cfgs...)

	return config, nil
}
