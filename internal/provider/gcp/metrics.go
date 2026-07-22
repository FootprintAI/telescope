package gcp

import (
	"context"
	"fmt"
	"time"

	monitoringpb "cloud.google.com/go/monitoring/apiv3/v2/monitoringpb"
	"google.golang.org/api/iterator"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/footprintai/telescope/internal/metricsutil"
	"github.com/footprintai/telescope/internal/model"
)

const alignSeconds = 300 // 5-minute alignment

// FetchMetrics fills each instance's Metrics from Cloud Monitoring over the
// window. Instances are matched to time series by numeric instance_id.
func (p *Provider) FetchMetrics(ctx context.Context, insts []model.Instance, w model.Window) ([]model.Instance, error) {
	if len(insts) == 0 {
		return insts, nil
	}
	// Accumulate per-instance sample slices across all projects.
	cpu := map[string][]float64{}
	mem := map[string][]float64{}
	net := map[string]map[int64]float64{} // id -> aligned-ts -> rx+tx bytes/s
	disk := map[string]map[int64]float64{}

	for _, project := range p.projects {
		mean := monitoringpb.Aggregation_ALIGN_MEAN
		rate := monitoringpb.Aggregation_ALIGN_RATE

		if err := p.collect(ctx, project, w, "compute.googleapis.com/instance/cpu/utilization", mean,
			func(id string, _ int64, v float64) { cpu[id] = append(cpu[id], v) }); err != nil {
			return nil, err
		}
		// Memory requires the Ops Agent; may return nothing.
		if err := p.collect(ctx, project, w, "agent.googleapis.com/memory/percent_used", mean,
			func(id string, _ int64, v float64) { mem[id] = append(mem[id], v/100.0) }); err != nil {
			return nil, err
		}
		if err := p.collect(ctx, project, w, "compute.googleapis.com/instance/network/received_bytes_count", rate,
			func(id string, ts int64, v float64) { addTS(net, id, ts, v) }); err != nil {
			return nil, err
		}
		if err := p.collect(ctx, project, w, "compute.googleapis.com/instance/network/sent_bytes_count", rate,
			func(id string, ts int64, v float64) { addTS(net, id, ts, v) }); err != nil {
			return nil, err
		}
		if err := p.collect(ctx, project, w, "compute.googleapis.com/instance/disk/read_ops_count", rate,
			func(id string, ts int64, v float64) { addTS(disk, id, ts, v) }); err != nil {
			return nil, err
		}
		if err := p.collect(ctx, project, w, "compute.googleapis.com/instance/disk/write_ops_count", rate,
			func(id string, ts int64, v float64) { addTS(disk, id, ts, v) }); err != nil {
			return nil, err
		}
	}

	for i := range insts {
		id := insts[i].ID
		m := &model.Metrics{
			CPUUtil:        metricsutil.Summarize(cpu[id]),
			MemUtil:        metricsutil.Summarize(mem[id]),
			NetBytesPerSec: metricsutil.Summarize(metricsutil.SumByKey(net[id])),
			DiskIOPS:       metricsutil.Summarize(metricsutil.SumByKey(disk[id])),
			Samples:        len(cpu[id]),
		}
		insts[i].Metrics = m
	}
	return insts, nil
}

// collect runs one ListTimeSeries query and invokes fn for every point.
func (p *Provider) collect(ctx context.Context, project string, w model.Window, metricType string, aligner monitoringpb.Aggregation_Aligner, fn func(id string, ts int64, v float64)) error {
	req := &monitoringpb.ListTimeSeriesRequest{
		Name:   "projects/" + project,
		Filter: fmt.Sprintf(`metric.type=%q AND resource.type="gce_instance"`, metricType),
		Interval: &monitoringpb.TimeInterval{
			StartTime: timestamppb.New(w.Start()),
			EndTime:   timestamppb.New(w.End),
		},
		Aggregation: &monitoringpb.Aggregation{
			AlignmentPeriod:  durationpb.New(alignSeconds * time.Second),
			PerSeriesAligner: aligner,
		},
		View: monitoringpb.ListTimeSeriesRequest_FULL,
	}
	it := p.metrics.ListTimeSeries(ctx, req)
	for {
		ts, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return fmt.Errorf("gcp: list time series %s (%s): %w", metricType, project, err)
		}
		id := ts.GetResource().GetLabels()["instance_id"]
		if id == "" {
			continue
		}
		for _, pt := range ts.GetPoints() {
			fn(id, pt.GetInterval().GetEndTime().GetSeconds(), pointValue(pt.GetValue()))
		}
	}
	return nil
}

// pointValue extracts a float from a monitoring TypedValue (double or int64).
func pointValue(tv *monitoringpb.TypedValue) float64 {
	switch v := tv.GetValue().(type) {
	case *monitoringpb.TypedValue_DoubleValue:
		return v.DoubleValue
	case *monitoringpb.TypedValue_Int64Value:
		return float64(v.Int64Value)
	default:
		return 0
	}
}

// addTS sums values that share an aligned timestamp (used to combine rx+tx and
// read+write into a single per-instant series before summarizing).
func addTS(m map[string]map[int64]float64, id string, ts int64, v float64) {
	if m[id] == nil {
		m[id] = map[int64]float64{}
	}
	m[id][ts] += v
}
