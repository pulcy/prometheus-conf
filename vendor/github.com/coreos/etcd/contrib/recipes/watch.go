// Copyright 2016 CoreOS, Inc.
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

package recipe

import (
	"github.com/coreos/etcd/Godeps/_workspace/src/golang.org/x/net/context"
	"github.com/coreos/etcd/clientv3"
	"github.com/coreos/etcd/storage/storagepb"
)

// WaitEvents waits on a key until it observes the given events and returns the final one.
func WaitEvents(c *clientv3.Client, key string, rev int64, evs []storagepb.Event_EventType) (*storagepb.Event, error) {
	w := clientv3.NewWatcher(c)
	wc := w.Watch(context.Background(), key, clientv3.WithRev(rev))
	if wc == nil {
		w.Close()
		return nil, ErrNoWatcher
	}
	return waitEvents(wc, evs), w.Close()
}

func WaitPrefixEvents(c *clientv3.Client, prefix string, rev int64, evs []storagepb.Event_EventType) (*storagepb.Event, error) {
	w := clientv3.NewWatcher(c)
	wc := w.Watch(context.Background(), prefix, clientv3.WithPrefix(), clientv3.WithRev(rev))
	if wc == nil {
		w.Close()
		return nil, ErrNoWatcher
	}
	return waitEvents(wc, evs), w.Close()
}

func waitEvents(wc clientv3.WatchChan, evs []storagepb.Event_EventType) *storagepb.Event {
	i := 0
	for wresp := range wc {
		for _, ev := range wresp.Events {
			if ev.Type == evs[i] {
				i++
				if i == len(evs) {
					return ev
				}
			}
		}
	}
	return nil
}
