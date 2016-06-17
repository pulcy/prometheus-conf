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
	"sync/atomic"
	"time"

	"github.com/go-yaml/yaml"
	"github.com/op/go-logging"
)

const (
	minRunInterval = time.Millisecond * 100
)

type ServiceConfig struct {
	ConfigPath string
	Once       bool
	LoopDelay  time.Duration
}

type ServiceDependencies struct {
	Log *logging.Logger
}
type Service struct {
	ServiceConfig
	ServiceDependencies

	updates    uint32
	lastConfig string
}

func NewService(config ServiceConfig, deps ServiceDependencies) *Service {
	return &Service{
		ServiceConfig:       config,
		ServiceDependencies: deps,
	}
}

// Run builds the config once of continuously
func (s *Service) Run() error {
	trigger := make(chan struct{})
	for _, p := range plugins {
		if err := p.Start(trigger); err != nil {
			return maskAny(err)
		}
	}

	go s.catchTriggers(trigger)
	s.updates = 1
	var lastUpdates uint32
	for {
		newUpdates := atomic.LoadUint32(&s.updates)
		if newUpdates != lastUpdates {
			lastUpdates = newUpdates
			err := s.runOnce()
			if err != nil {
				s.Log.Errorf("runOnce failed: %#v", err)
			}
			if s.Once {
				return maskAny(err)
			}
		} else {
			time.Sleep(minRunInterval)
		}
	}
}

// catchTriggers increments an updates counter for every received trigger and at every loop interval.
// This updates counter is used to debounce events and avoid updating the configuration to often.
func (s *Service) catchTriggers(trigger chan struct{}) {
	interval := time.NewTicker(s.LoopDelay)
	for {
		select {
		case <-trigger:
			atomic.AddUint32(&s.updates, 1)
		case <-interval.C:
			atomic.AddUint32(&s.updates, 1)
		}
	}
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
	newConfig := string(raw)
	if newConfig != s.lastConfig {
		if err := ioutil.WriteFile(s.ConfigPath, raw, 0755); err != nil {
			return maskAny(err)
		}
		s.lastConfig = newConfig
	}

	return nil
}

// createConfig builds he configuration file (in memory)
func (s *Service) createConfig() (PrometheusConfig, error) {
	config := PrometheusConfig{}

	// Let all plugins create their nodes
	for _, p := range plugins {
		cfgs, err := p.CreateNodes()
		if err != nil {
			return config, maskAny(err)
		}
		config.ScrapeConfigs = append(config.ScrapeConfigs, cfgs...)
	}

	return config, nil
}
