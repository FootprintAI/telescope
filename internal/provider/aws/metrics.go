package aws

import (
	"context"
	"fmt"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"

	"github.com/footprintai/telescope/internal/metricsutil"
	"github.com/footprintai/telescope/internal/model"
)

const (
	cwPeriod   = 300 // 5-minute alignment, matching the GCP provider
	cwMaxBatch = 80  // instances per GetMetricData call (<= ~480 queries)
)

// FetchMetrics fills each instance's Metrics from CloudWatch over the window.
// Instances are queried per region and matched back by array index.
func (p *Provider) FetchMetrics(ctx context.Context, insts []model.Instance, w model.Window) ([]model.Instance, error) {
	byRegion := map[string][]int{}
	for i, in := range insts {
		byRegion[in.Region] = append(byRegion[in.Region], i)
	}
	for region, idxs := range byRegion {
		cli := cloudwatch.NewFromConfig(p.base, func(o *cloudwatch.Options) { o.Region = region })
		for start := 0; start < len(idxs); start += cwMaxBatch {
			end := start + cwMaxBatch
			if end > len(idxs) {
				end = len(idxs)
			}
			if err := p.fetchBatch(ctx, cli, insts, idxs[start:end], w); err != nil {
				return nil, err
			}
		}
	}
	return insts, nil
}

// cwKind identifies which dimension a query result feeds.
type cwKind int

const (
	kindCPU cwKind = iota
	kindMem
	kindNetIn
	kindNetOut
	kindEBSRead
	kindEBSWrite
)

// metricStatSpec drives an AWS/EC2 MetricStat query.
type metricStatSpec struct {
	kind      cwKind
	namespace string
	metric    string
	stat      string
}

var ec2Specs = []metricStatSpec{
	{kindCPU, "AWS/EC2", "CPUUtilization", "Average"},
	{kindNetIn, "AWS/EC2", "NetworkIn", "Sum"},
	{kindNetOut, "AWS/EC2", "NetworkOut", "Sum"},
	{kindEBSRead, "AWS/EC2", "EBSReadOps", "Sum"},
	{kindEBSWrite, "AWS/EC2", "EBSWriteOps", "Sum"},
}

func (p *Provider) fetchBatch(ctx context.Context, cli *cloudwatch.Client, insts []model.Instance, idxs []int, w model.Window) error {
	var queries []cwtypes.MetricDataQuery
	meta := map[string]struct {
		instIdx int
		kind    cwKind
	}{}

	for slot, i := range idxs {
		id := insts[i].ID
		// AWS/EC2 metrics: exact InstanceId dimension.
		for j, spec := range ec2Specs {
			qid := fmt.Sprintf("q%d_%d", slot, j)
			meta[qid] = struct {
				instIdx int
				kind    cwKind
			}{i, spec.kind}
			queries = append(queries, cwtypes.MetricDataQuery{
				Id:         awssdk.String(qid),
				ReturnData: awssdk.Bool(true),
				MetricStat: &cwtypes.MetricStat{
					Period: awssdk.Int32(cwPeriod),
					Stat:   awssdk.String(spec.stat),
					Metric: &cwtypes.Metric{
						Namespace:  awssdk.String(spec.namespace),
						MetricName: awssdk.String(spec.metric),
						Dimensions: []cwtypes.Dimension{{
							Name:  awssdk.String("InstanceId"),
							Value: awssdk.String(id),
						}},
					},
				},
			})
		}
		// Memory (CWAgent) via SEARCH so extra agent dimensions still match.
		memID := fmt.Sprintf("m%d", slot)
		meta[memID] = struct {
			instIdx int
			kind    cwKind
		}{i, kindMem}
		expr := fmt.Sprintf(
			`SEARCH('{CWAgent,InstanceId} MetricName="mem_used_percent" InstanceId="%s"', 'Average', %d)`,
			id, cwPeriod)
		queries = append(queries, cwtypes.MetricDataQuery{
			Id:         awssdk.String(memID),
			ReturnData: awssdk.Bool(true),
			Expression: awssdk.String(expr),
		})
	}

	// Collect per-instance raw samples.
	cpu := map[int][]float64{}
	mem := map[int][]float64{}
	net := map[int]map[int64]float64{}
	ebs := map[int]map[int64]float64{}

	pager := cloudwatch.NewGetMetricDataPaginator(cli, &cloudwatch.GetMetricDataInput{
		StartTime:         awssdk.Time(w.Start()),
		EndTime:           awssdk.Time(w.End),
		ScanBy:            cwtypes.ScanByTimestampAscending,
		MetricDataQueries: queries,
	})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("aws: get metric data: %w", err)
		}
		for _, r := range page.MetricDataResults {
			m, ok := meta[awssdk.ToString(r.Id)]
			if !ok {
				continue
			}
			for k := range r.Values {
				v := r.Values[k]
				ts := r.Timestamps[k].Unix()
				switch m.kind {
				case kindCPU:
					cpu[m.instIdx] = append(cpu[m.instIdx], v/100.0)
				case kindMem:
					mem[m.instIdx] = append(mem[m.instIdx], v/100.0)
				case kindNetIn, kindNetOut:
					addTS(net, m.instIdx, ts, v/float64(cwPeriod)) // bytes/sec
				case kindEBSRead, kindEBSWrite:
					addTS(ebs, m.instIdx, ts, v/float64(cwPeriod)) // ops/sec
				}
			}
		}
	}

	for _, i := range idxs {
		insts[i].Metrics = &model.Metrics{
			CPUUtil:        metricsutil.Summarize(cpu[i]),
			MemUtil:        metricsutil.Summarize(mem[i]),
			NetBytesPerSec: metricsutil.Summarize(metricsutil.SumByKey(net[i])),
			DiskIOPS:       metricsutil.Summarize(metricsutil.SumByKey(ebs[i])),
			Samples:        len(cpu[i]),
		}
	}
	return nil
}

// addTS sums values sharing a timestamp (combines rx+tx / read+write).
func addTS(m map[int]map[int64]float64, i int, ts int64, v float64) {
	if m[i] == nil {
		m[i] = map[int64]float64{}
	}
	m[i][ts] += v
}
