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

package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/op/go-logging"
	"github.com/spf13/cobra"

	"github.com/pulcy/prometheus-conf/service"
)

const (
	projectName             = "prometheus-conf"
	defaultConfigPath       = "./prometheus.yml"
	defaultNodeExporterPort = 9102
)

var (
	defaultLoopDelay = time.Second * 30
	defaultLogLevel  = "info"

	projectVersion = "dev"
	projectBuild   = "dev"
)

var (
	cmdMain = &cobra.Command{
		Use:   projectName,
		Short: "Build prometheus configuration file for a Pulcy cluster",
		Run:   cmdMainRun,
	}
	log   = logging.MustGetLogger(projectName)
	flags struct {
		logLevel string
		service.ServiceConfig
	}
)

func init() {
	logging.SetFormatter(logging.MustStringFormatter("[%{level:-5s}] %{message}"))
	cmdMain.Flags().StringVar(&flags.logLevel, "log-level", defaultLogLevel, "Log level (debug|info|warning|error)")
	cmdMain.Flags().StringVar(&flags.ConfigPath, "config-path", defaultConfigPath, "Path of the generated config file")
	cmdMain.Flags().BoolVar(&flags.Once, "once", false, "If set, the config ill be generated only once")
	cmdMain.Flags().DurationVar(&flags.LoopDelay, "loop-delay", defaultLoopDelay, "Time to wait before rebuilding the config file")
	cmdMain.Flags().StringVar(&flags.FleetURL, "fleet-url", "", "URL of fleet")
	cmdMain.Flags().IntVar(&flags.NodeExporterPort, "node-exporter-port", defaultNodeExporterPort, "Port that node_exporters are listening on")
}

func main() {
	cmdMain.Execute()
}

func cmdMainRun(cmd *cobra.Command, args []string) {
	setLogLevel(flags.logLevel)
	s := service.NewService(flags.ServiceConfig, service.ServiceDependencies{
		Log: log,
	})
	if err := s.Run(); err != nil {
		Exitf("Config creation failed: %#v", err)
	}
}

func Exitf(format string, args ...interface{}) {
	if !strings.HasSuffix(format, "\n") {
		format = format + "\n"
	}
	fmt.Printf(format, args...)
	os.Exit(1)
}

func def(envKey, defaultValue string) string {
	s := os.Getenv(envKey)
	if s == "" {
		s = defaultValue
	}
	return s
}

func setLogLevel(logLevel string) {
	level, err := logging.LogLevel(logLevel)
	if err != nil {
		Exitf("Invalid log-level '%s': %#v", logLevel, err)
	}
	logging.SetLevel(level, projectName)
}
