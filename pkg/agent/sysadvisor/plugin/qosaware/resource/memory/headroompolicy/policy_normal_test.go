/*
Copyright 2022 The Katalyst Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package headroompolicy

import (
	"io/ioutil"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/kubewharf/katalyst-api/pkg/consts"
	"github.com/kubewharf/katalyst-core/pkg/agent/sysadvisor/metacache"
	"github.com/kubewharf/katalyst-core/pkg/agent/sysadvisor/types"
	pkgconsts "github.com/kubewharf/katalyst-core/pkg/consts"
	"github.com/kubewharf/katalyst-core/pkg/metaserver/agent/metric"
	"github.com/kubewharf/katalyst-core/pkg/metrics"
	metricspool "github.com/kubewharf/katalyst-core/pkg/metrics/metrics-pool"
	utilmetric "github.com/kubewharf/katalyst-core/pkg/util/metric"
)

func TestPolicyNormal(t *testing.T) {
	t.Parallel()

	now := time.Now()

	type fields struct {
		podList       []*v1.Pod
		containers    []*types.ContainerInfo
		essentials    types.ResourceEssentials
		setFakeMetric func(store *metric.FakeMetricsFetcher)
	}
	tests := []struct {
		name    string
		fields  fields
		want    resource.Quantity
		wantErr bool
	}{
		{
			name: "normal",
			fields: fields{
				podList: []*v1.Pod{},
				containers: []*types.ContainerInfo{
					makeContainerInfo("pod1", "default",
						"pod1", "container1",
						consts.PodAnnotationQoSLevelReclaimedCores, nil,
						nil, 20<<30),
					makeContainerInfo("pod2", "default",
						"pod2", "container1",
						consts.PodAnnotationQoSLevelReclaimedCores, nil,
						nil, 20<<30),
				},
				essentials: types.ResourceEssentials{
					EnableReclaim:       true,
					ResourceUpperBound:  400 << 30,
					ReservedForAllocate: 4 << 30,
				},
				setFakeMetric: func(store *metric.FakeMetricsFetcher) {
					store.SetNodeMetric(pkgconsts.MetricMemFreeSystem, utilmetric.MetricData{Value: 100 << 30, Time: &now})
				},
			},
			wantErr: false,
			want:    resource.MustParse("136Gi"),
		},
		{
			name: "empty reclaimed-core containers and free is below reserved",
			fields: fields{
				podList:    []*v1.Pod{},
				containers: []*types.ContainerInfo{},
				essentials: types.ResourceEssentials{
					EnableReclaim:       true,
					ResourceUpperBound:  400 << 30,
					ReservedForAllocate: 40 << 30,
				},
				setFakeMetric: func(store *metric.FakeMetricsFetcher) {
					store.SetNodeMetric(pkgconsts.MetricMemFreeSystem, utilmetric.MetricData{Value: 20 << 30, Time: &now})
				},
			},
			wantErr: false,
			want:    resource.MustParse("0Gi"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ckDir, err := ioutil.TempDir("", "checkpoint-TestPolicyNormal")
			require.NoError(t, err)
			defer os.RemoveAll(ckDir)

			sfDir, err := ioutil.TempDir("", "statefile")
			require.NoError(t, err)
			defer os.RemoveAll(sfDir)

			conf := generateTestConfiguration(t, ckDir, sfDir)

			metricsFetcher := metric.NewFakeMetricsFetcher(metrics.DummyMetrics{})
			metaCache, err := metacache.NewMetaCacheImp(conf, metricspool.DummyMetricsEmitterPool{}, metricsFetcher)
			require.NoError(t, err)

			for _, c := range tt.fields.containers {
				err := metaCache.SetContainerInfo(c.PodUID, c.ContainerName, c)
				assert.NoError(t, err)
			}

			metaServer := generateTestMetaServer(t, tt.fields.podList, metricsFetcher)

			p := NewPolicyNormal(conf, nil, metaCache, metaServer, metrics.DummyMetrics{})

			store := metricsFetcher.(*metric.FakeMetricsFetcher)
			tt.fields.setFakeMetric(store)

			p.SetEssentials(tt.fields.essentials)

			err = p.Update()
			if (err != nil) != tt.wantErr {
				t.Errorf("update() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			got, err := p.GetHeadroom()
			if (err != nil) != tt.wantErr {
				t.Errorf("GetHeadroom() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got.MilliValue() != tt.want.MilliValue() {
				t.Errorf("GetHeadroom() got = %v, want %v", got, tt.want)
			}
		})
	}
}
