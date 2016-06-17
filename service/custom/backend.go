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

package custom

import (
	"encoding/json"
	"fmt"

	"github.com/coreos/etcd/client"
	"github.com/op/go-logging"
	"github.com/pulcy/prometheus-conf-api"
	"golang.org/x/net/context"
)

const (
	recentWatchErrorsMax = 5
)

type etcdBackend struct {
	client            client.Client
	watcher           client.Watcher
	Logger            *logging.Logger
	prefix            string
	recentWatchErrors int
}

// newEtcdBackend creates a new metrics API client from the given arguments.
// The etcdClient is required, all other arguments are options and will be set to default values if not given.
func newEtcdBackend(etcdClient client.Client, etcdPath string, logger *logging.Logger) (*etcdBackend, error) {
	if etcdPath == "" {
		etcdPath = defaultEtcdPath
	}
	return &etcdBackend{
		client: etcdClient,
		prefix: etcdPath,
		Logger: logger,
	}, nil
}

// Watch for changes on a path and return where there is a change.
func (b *etcdBackend) Watch() error {
	if b.watcher == nil || b.recentWatchErrors > recentWatchErrorsMax {
		b.recentWatchErrors = 0
		keyAPI := client.NewKeysAPI(b.client)
		options := &client.WatcherOptions{
			Recursive: true,
		}
		b.watcher = keyAPI.Watcher(b.prefix, options)
	}
	_, err := b.watcher.Next(context.Background())
	if err != nil {
		b.recentWatchErrors++
		return maskAny(err)
	}
	b.recentWatchErrors = 0
	return nil
}

// Load all registered metrics
func (b *etcdBackend) Metrics() ([]api.MetricsServiceRecord, error) {
	keyAPI := client.NewKeysAPI(b.client)
	options := &client.GetOptions{
		Recursive: false,
		Sort:      true,
	}
	resp, err := keyAPI.Get(context.Background(), b.prefix, options)
	if err != nil {
		if isEtcdKeyNotFound(err) {
			// Just no custom metrics
			return nil, nil
		}
		return nil, maskAny(err)
	}
	var list []api.MetricsServiceRecord
	if resp.Node == nil {
		return list, nil
	}
	found := make(map[string]struct{})
	for _, metricsNode := range resp.Node.Nodes {
		var record api.MetricsServiceRecord
		if err := json.Unmarshal([]byte(metricsNode.Value), &record); err != nil {
			b.Logger.Warningf("Cannot failed metrics node '%s': %v", metricsNode.Key, err)
			continue
		}
		key := fmt.Sprintf("%s-%d", record.ServiceName, record.ServicePort)
		if _, ok := found[key]; !ok {
			found[key] = struct{}{}
			list = append(list, record)
		}
	}

	b.Logger.Debugf("Found %d distinct metrics records", len(list))
	return list, nil
}

func isEtcdKeyNotFound(err error) bool {
	if etcdErr, ok := err.(client.Error); ok {
		return etcdErr.Code == client.ErrorCodeKeyNotFound
	}
	return false
}
