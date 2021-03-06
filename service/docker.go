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
	"github.com/cenkalti/backoff"
	"github.com/fsouza/go-dockerclient"
)

// reloadPrometheusConfigurationUsingDocker sends a HUP signal to the specified docker container
// running prometheus to reload the configuration.
func (s *Service) reloadPrometheusConfigurationUsingDocker() error {
	if s.PrometheusContainerName == "" {
		return nil
	}

	s.Log.Debugf("sending SIGHUP to %s", s.PrometheusContainerName)
	d, err := docker.NewClientFromEnv()
	if err != nil {
		return maskAny(err)
	}

	op := func() error {
		opts := docker.KillContainerOptions{
			ID:     s.PrometheusContainerName,
			Signal: docker.SIGHUP,
		}
		return maskAny(d.KillContainer(opts))
	}
	if err := backoff.Retry(op, backoff.NewExponentialBackOff()); err != nil {
		return maskAny(err)
	}

	return nil
}
