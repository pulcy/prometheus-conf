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
	"github.com/spf13/pflag"
)

type Plugin interface {
	// Configure the command line flags needed by the plugin.
	Setup(flagSet *pflag.FlagSet)

	// Start the plugin. Send a value on the given channel to trigger an update of the configuration.
	// Use a go-routine internally since this method is blocking.
	Start(config ServiceConfig, trigger chan struct{}) error

	// CreateNodes creates all scrape configurations this plugin is aware of.
	CreateNodes() ([]ScrapeConfig, error)
}

var (
	plugins = make(map[string]Plugin)
)

func RegisterPlugin(name string, plugin Plugin) {
	plugins[name] = plugin
}

func SetupPlugins(flagSet *pflag.FlagSet) {
	for _, p := range plugins {
		p.Setup(flagSet)
	}
}
