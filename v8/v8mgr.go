// Copyright 2020-present, lizc2003@gmail.com
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

package v8

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/magicalcosmos/goblogssr/common/tlog"
	"github.com/magicalcosmos/goblogssr/v8worker"
)

const (
	DELETE_DALAY_TIME = 2 * time.Minute
)

type V8SendCallback func(msgType int, msg string, userdata int64)

type V8MgrConfig struct {
	Env             string
	JsPaths         []string
	MaxWorkerCount  int32
	WorkerLifeTime  int
	InternalApiHost string
	InternalApiIp   string
	InternalApiPort int32
	SendCallback    V8SendCallback
}

type V8Mgr struct {
	env                string
	httpMgr            *xmlHttpRequestMgr
	SendCallback       V8SendCallback
	workers            chan *v8worker.Worker
	workerLifeTime     int64
	maxWorkerCount     int32
	currentWorkerCount int32
}

var TheV8Mgr *V8Mgr

func NewV8Mgr(c *V8MgrConfig) (*V8Mgr, error) {
	initV8Module(c.JsPaths)
	initV8NewJs()

	TheV8Mgr = &V8Mgr{env: c.Env,
		httpMgr:        NewXmlHttpRequestMgr(int(c.MaxWorkerCount)*2, c.InternalApiHost, c.InternalApiIp, c.InternalApiPort),
		SendCallback:   c.SendCallback,
		workerLifeTime: int64(c.WorkerLifeTime),
		maxWorkerCount: c.MaxWorkerCount}

	worker, err := newV8Worker(c.Env)
	if err != nil {
		return nil, err
	}

	worker.SetExpireTime(time.Now().Unix() + int64(c.WorkerLifeTime))
	workers := make(chan *v8worker.Worker, c.MaxWorkerCount+100)
	workers <- worker

	TheV8Mgr.workers = workers
	TheV8Mgr.currentWorkerCount = 1
	return TheV8Mgr, nil
}

// Execute executes
func (that *V8Mgr) Execute(name string, code string) error {
	w := that.acquireWorker()
	err := w.Execute(name, code)
	if err != nil {
		tlog.Error(err)
	}
	that.releaseWorker(w)
	return err
}

// GetInternelApiUrl get internel API url
func (that *V8Mgr) GetInternelApiUrl() string {
	if that.httpMgr.internalApiHost != "" {
		return fmt.Sprintf("http://%s:%d", that.httpMgr.internalApiHost, that.httpMgr.internalApiPort)
	}
	return ""
}

func (this *V8Mgr) acquireWorker() *v8worker.Worker {
	var busyWorkers []*v8worker.Worker
	for {
		var ret *v8worker.Worker
		bEmpty := false
		select {
		case worker := <-this.workers:
			if worker.Acquire() {
				ret = worker
			} else {
				busyWorkers = append(busyWorkers, worker)
			}
		default:
			if this.currentWorkerCount < this.maxWorkerCount {
				worker, err := newV8Worker(this.env)
				if err == nil {
					atomic.AddInt32(&this.currentWorkerCount, 1)
					worker.SetExpireTime(time.Now().Unix() + this.workerLifeTime)
					worker.Acquire()
					ret = worker
				} else {
					bEmpty = true
				}
			} else {
				bEmpty = true
			}
		}

		if ret != nil {
			for _, w := range busyWorkers {
				this.workers <- w
			}
			return ret
		} else if bEmpty {
			if len(busyWorkers) > 0 {
				for _, w := range busyWorkers {
					this.workers <- w
				}
				busyWorkers = busyWorkers[:0]
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func (this *V8Mgr) releaseWorker(worker *v8worker.Worker) {
	if worker != nil {
		worker.Release()

		if time.Now().Unix() >= worker.GetExpireTime() {
			atomic.AddInt32(&this.currentWorkerCount, -1)

			go func(w *v8worker.Worker) {
				time.Sleep(DELETE_DALAY_TIME)
				w.Dispose()
			}(worker)
		} else {
			this.workers <- worker
		}
	}
}
