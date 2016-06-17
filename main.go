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
	"github.com/pulcy/prometheus-conf/util"
)

const (
	projectName       = "prometheus-conf"
	defaultConfigPath = "./prometheus.yml"
	defaultLoopDelay  = time.Second * 30
	defaultLogLevel   = "info"
)

var (
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
		service.ServiceConfig
	}
)

func init() {
	logging.SetFormatter(logging.MustStringFormatter("[%{level:-5s}] %{message}"))
	cmdMain.Flags().StringVar(&flags.LogLevel, "log-level", defaultLogLevel, "Log level (debug|info|warning|error)")
	cmdMain.Flags().StringVar(&flags.ConfigPath, "config-path", defaultConfigPath, "Path of the generated config file")
	cmdMain.Flags().BoolVar(&flags.Once, "once", false, "If set, the config will be generated only once")
	cmdMain.Flags().DurationVar(&flags.LoopDelay, "loop-delay", defaultLoopDelay, "Time to wait before rebuilding the config file")
}

func main() {
	service.SetupPlugins(cmdMain.Flags())
	cmdMain.Execute()
}

func cmdMainRun(cmd *cobra.Command, args []string) {
	if err := util.SetLogLevel(flags.LogLevel, defaultLogLevel, projectName); err != nil {
		Exitf("Failed to set log-level: %#v", err)
	}
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
