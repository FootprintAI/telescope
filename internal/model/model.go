// Package model holds the provider-agnostic types that flow through the
// inventory -> metrics -> analyze -> binpack -> recommend pipeline.
package model

import "time"

// Window describes the metrics lookback period.
type Window struct {
	Lookback time.Duration
	End      time.Time
}

// Start returns the beginning of the lookback window.
func (w Window) Start() time.Time { return w.End.Add(-w.Lookback) }

// Disk is a single attached block device.
type Disk struct {
	Name    string
	SizeGB  float64
	Type    string // e.g. pd-ssd, pd-standard, gp3
	IOPSCap float64
}

// ProvisioningModel is how an instance's capacity is provisioned and billed.
// Spot/preemptible capacity is billed at different rates than on-demand, so
// pricing must know which one an instance uses.
type ProvisioningModel string

const (
	ProvisioningOnDemand ProvisioningModel = "on-demand"
	ProvisioningSpot     ProvisioningModel = "spot"
)

// Instance is a single VM (GCE instance or EC2 instance).
type Instance struct {
	ID                string
	Name              string
	Provider          string // "gcp" | "aws" | "mock"
	Project           string // GCP project / AWS account
	Region            string
	Zone              string
	MachineType       string            // e.g. e2-standard-4, m5.xlarge
	ProvisioningModel ProvisioningModel // "" = unknown
	VCPU              float64           // logical vCPUs
	MemGB             float64
	NICGbps           float64 // NIC line-rate ceiling in Gbit/s (0 = unknown)
	Disks             []Disk
	Labels            map[string]string

	// Metrics is filled in by Provider.FetchMetrics. Nil until then.
	Metrics *Metrics
}

// DiskGB is the sum of all attached disk sizes.
func (i Instance) DiskGB() float64 {
	var t float64
	for _, d := range i.Disks {
		t += d.SizeGB
	}
	return t
}

// DiskIOPSCap is the aggregate IOPS ceiling across attached disks.
func (i Instance) DiskIOPSCap() float64 {
	var t float64
	for _, d := range i.Disks {
		t += d.IOPSCap
	}
	return t
}

// Stat holds summary statistics for one metric dimension over the window.
type Stat struct {
	P50 float64
	P95 float64
	Max float64
	// Present is false when the source metric was unavailable (e.g. EC2/GCE
	// memory without a monitoring agent installed).
	Present bool
}

// Metrics holds per-dimension utilization summaries for one instance.
//
// CPUUtil and MemUtil are fractions in [0,1]. NetBytesPerSec is aggregate
// (rx+tx) throughput. DiskIOPS is aggregate read+write ops/sec.
type Metrics struct {
	CPUUtil        Stat // fraction 0..1
	MemUtil        Stat // fraction 0..1
	NetBytesPerSec Stat // bytes/sec
	DiskIOPS       Stat // ops/sec
	Samples        int  // number of data points backing these stats
}

// NodePool is a GKE/EKS nodepool (inventory only for the MVP).
type NodePool struct {
	Cluster     string
	Name        string
	Provider    string
	Region      string
	MachineType string
	VCPU        float64
	MemGB       float64
	NodeCount   int
}
