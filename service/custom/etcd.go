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
	"net/url"

	"github.com/coreos/etcd/client"
)

// newEtcdClient creates a ETCD client from the given URL.
// On success it returns the new client and the path into the ETCD namespace.
func newEtcdClient(etcdURL string) (client.Client, string, error) {
	uri, err := url.Parse(etcdURL)
	if err != nil {
		return nil, "", maskAny(err)
	}
	cfg := client.Config{
		Transport: client.DefaultTransport,
	}
	if uri.Host != "" {
		cfg.Endpoints = append(cfg.Endpoints, "http://"+uri.Host)
	}
	c, err := client.New(cfg)
	if err != nil {
		return nil, "", maskAny(err)
	}
	return c, uri.Path, nil
}
