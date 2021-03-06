// Copyright 2015 CoreOS, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package registry

import (
	"path"

	etcd "github.com/coreos/etcd/client"
)

const DefaultKeyPrefix = "/_coreos.com/fleet/"

func NewEtcdRegistry(kAPI etcd.KeysAPI, keyPrefix string) *EtcdRegistry {
	return &EtcdRegistry{
		kAPI:      kAPI,
		keyPrefix: keyPrefix,
	}
}

// EtcdRegistry fulfils the Registry interface and uses etcd as a backend
type EtcdRegistry struct {
	kAPI      etcd.KeysAPI
	keyPrefix string
}

func (r *EtcdRegistry) prefixed(p ...string) string {
	return path.Join(r.keyPrefix, path.Join(p...))
}

func isEtcdError(err error, code int) bool {
	eerr, ok := err.(etcd.Error)
	return ok && eerr.Code == code
}
