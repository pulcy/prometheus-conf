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
	"crypto/sha1"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-yaml/yaml"
	"github.com/op/go-logging"
)

const (
	minRunInterval                     = time.Second * 5
	reloadPrometheusConfigurationDelay = time.Second * 30
)

type ServiceConfig struct {
	LogLevel                string
	ConfigPath              string
	RulesPath               string
	Once                    bool
	LoopDelay               time.Duration
	PrometheusContainerName string
	PrometheusReloadURL     string
	NodeExporterPort        int
}

type ServiceDependencies struct {
	Log *logging.Logger
}
type Service struct {
	ServiceConfig
	ServiceDependencies

	updates    uint32
	lastConfig string
	startTime  time.Time
}

func NewService(config ServiceConfig, deps ServiceDependencies) *Service {
	if config.RulesPath == "" {
		config.RulesPath = filepath.Join(filepath.Dir(config.ConfigPath), "rules")
	}
	return &Service{
		ServiceConfig:       config,
		ServiceDependencies: deps,
		startTime:           time.Now(),
	}
}

// Run builds the config once of continuously
func (s *Service) Run() error {
	trigger := make(chan string)
	for _, p := range plugins {
		if err := p.Start(s.ServiceConfig, trigger); err != nil {
			return maskAny(err)
		}
	}

	go s.catchTriggers(trigger)
	s.updates = 1
	var lastUpdates uint32
	for {
		newUpdates := atomic.LoadUint32(&s.updates)
		if newUpdates != lastUpdates {
			s.Log.Debugf("updates has changed, calling runOnce (%v -> %v)", lastUpdates, newUpdates)
			lastUpdates = newUpdates
			err := s.runOnce()
			if err != nil {
				s.Log.Errorf("runOnce failed: %#v", err)
			}
			if s.Once {
				return maskAny(err)
			}
		}
		// Always wait a bit so we do not hammer the infrastructure
		time.Sleep(minRunInterval)
	}
}

// catchTriggers increments an updates counter for every received trigger and at every loop interval.
// This updates counter is used to debounce events and avoid updating the configuration to often.
func (s *Service) catchTriggers(trigger chan string) {
	interval := time.NewTicker(s.LoopDelay)
	for {
		select {
		case source := <-trigger:
			s.Log.Debugf("trigger received from '%s'", source)
			atomic.AddUint32(&s.updates, 1)
		case <-interval.C:
			s.Log.Debugf("loop interval")
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
		s.Log.Infof("Updating %s", s.ConfigPath)
		if err := ioutil.WriteFile(s.ConfigPath, raw, 0755); err != nil {
			return maskAny(err)
		}
		s.lastConfig = newConfig

		// Reload configuration
		if s.canReloadPrometheusConfiguration() {
			if err := s.reloadPrometheusConfiguration(); err != nil {
				s.Log.Errorf("Failed to reload prometheus configuration: %#v", err)
			}
		}
	}

	return nil
}

// createConfig builds he configuration file (in memory)
func (s *Service) createConfig() (PrometheusConfig, error) {
	config := PrometheusConfig{
		Global: GlobalConfig{
			ScrapeInterval: "2m",
		},
	}
	allRules := make(map[string]string)

	// Let all plugins create their nodes
	for _, p := range plugins {
		update, err := p.Update()
		if err != nil {
			return config, maskAny(err)
		}
		if update == nil {
			// Nothing to do for this plugin
			continue
		}

		// Create nodes
		cfgs, err := update.CreateNodes()
		if err != nil {
			return config, maskAny(err)
		}
		config.ScrapeConfigs = append(config.ScrapeConfigs, cfgs...)

		// Create rules
		rules, err := update.CreateRules()
		if err != nil {
			return config, maskAny(err)
		}
		for _, rule := range rules {
			hash := fmt.Sprintf("%x", sha1.Sum([]byte(rule)))
			allRules[hash] = rule
		}
	}

	// Store all rule files
	for hash, rule := range allRules {
		rulePath := filepath.Join(s.RulesPath, hash)
		if _, err := os.Stat(rulePath); err != nil {
			if err := ioutil.WriteFile(rulePath, []byte(rule), 0644); err != nil {
				return config, maskAny(err)
			}
			s.Log.Debugf("Written rules file %s", rulePath)
		}
		config.RuleFiles = append(config.RuleFiles, rulePath)
	}

	config.Sort()

	return config, nil
}

// canReloadPrometheusConfiguration returns true if it is safe to send a SIGHUP to
// prometheus.
// It prevents this until 30 seconds after startup.
func (s *Service) canReloadPrometheusConfiguration() bool {
	return time.Since(s.startTime) > reloadPrometheusConfigurationDelay
}

// reloadPrometheusConfiguration sends a HUP signal to the specified docker container
// running prometheus to reload the configuration.
func (s *Service) reloadPrometheusConfiguration() error {
	if s.PrometheusReloadURL != "" {
		body := strings.NewReader("{}")
		if _, err := http.Post(s.PrometheusReloadURL, "application/json", body); err != nil {
			return maskAny(err)
		}
	} else if s.PrometheusContainerName != "" {
		if err := s.reloadPrometheusConfigurationUsingDocker(); err != nil {
			return maskAny(err)
		}
	}
	s.Log.Debugf("Cannot reload prometheus due to lack of configuration")
	return nil
}
