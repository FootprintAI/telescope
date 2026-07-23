// Package gcp implements a read-only Provider over Google Cloud: Compute Engine
// instances, GKE nodepools (inventory), and Cloud Monitoring metrics.
//
// Required read-only roles on the service account:
//
//	roles/compute.viewer, roles/container.viewer, roles/monitoring.viewer
package gcp

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	compute "cloud.google.com/go/compute/apiv1"
	computepb "cloud.google.com/go/compute/apiv1/computepb"
	container "cloud.google.com/go/container/apiv1"
	containerpb "cloud.google.com/go/container/apiv1/containerpb"
	monitoring "cloud.google.com/go/monitoring/apiv3/v2"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"

	"github.com/footprintai/telescope/internal/model"
	"github.com/footprintai/telescope/internal/provider"
)

// Provider is a GCP read-only backend.
type Provider struct {
	projects []string
	regions  map[string]bool // empty => all

	instances *compute.InstancesClient
	machines  *compute.MachineTypesClient
	disks     *compute.DisksClient
	clusters  *container.ClusterManagerClient
	metrics   *monitoring.MetricClient

	// machineType cache keyed by "zone/name" -> (vCPU, memGB)
	mtCache map[string]machineSpec
}

type machineSpec struct {
	vCPU  float64
	memGB float64
}

// New builds a GCP provider. Credentials come from cfg.CredentialsFile when set,
// otherwise Application Default Credentials.
func New(ctx context.Context, cfg provider.Config) (*Provider, error) {
	if len(cfg.Projects) == 0 {
		return nil, fmt.Errorf("gcp: at least one --projects value is required")
	}
	var opts []option.ClientOption
	if cfg.CredentialsFile != "" {
		opts = append(opts, option.WithCredentialsFile(cfg.CredentialsFile))
	}

	inst, err := compute.NewInstancesRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("gcp: instances client: %w", err)
	}
	mt, err := compute.NewMachineTypesRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("gcp: machine-types client: %w", err)
	}
	dk, err := compute.NewDisksRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("gcp: disks client: %w", err)
	}
	cl, err := container.NewClusterManagerClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("gcp: container client: %w", err)
	}
	mc, err := monitoring.NewMetricClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("gcp: monitoring client: %w", err)
	}

	regions := map[string]bool{}
	for _, r := range cfg.Regions {
		regions[r] = true
	}
	return &Provider{
		projects:  cfg.Projects,
		regions:   regions,
		instances: inst,
		machines:  mt,
		disks:     dk,
		clusters:  cl,
		metrics:   mc,
		mtCache:   map[string]machineSpec{},
	}, nil
}

// Name implements provider.Provider.
func (p *Provider) Name() string { return "gcp" }

// ListInstances enumerates Compute Engine VMs across the configured projects.
func (p *Provider) ListInstances(ctx context.Context) ([]model.Instance, error) {
	var out []model.Instance
	for _, project := range p.projects {
		diskIdx, err := p.loadDiskIndex(ctx, project)
		if err != nil {
			return nil, err
		}
		it := p.instances.AggregatedList(ctx, &computepb.AggregatedListInstancesRequest{
			Project: project,
		})
		for {
			pair, err := it.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("gcp: aggregated list instances (%s): %w", project, err)
			}
			for _, vm := range pair.Value.GetInstances() {
				zone := lastSegment(vm.GetZone())
				region := regionOfZone(zone)
				if !p.regionAllowed(region) {
					continue
				}
				if strings.EqualFold(vm.GetStatus(), "TERMINATED") {
					continue
				}
				in := p.toInstance(ctx, project, zone, region, vm, diskIdx)
				out = append(out, in)
			}
		}
	}
	return out, nil
}

// loadDiskIndex fetches every persistent disk in a project once, keyed by
// "zone/name", so per-instance disk lookups need no extra API calls.
func (p *Provider) loadDiskIndex(ctx context.Context, project string) (map[string]*computepb.Disk, error) {
	idx := map[string]*computepb.Disk{}
	it := p.disks.AggregatedList(ctx, &computepb.AggregatedListDisksRequest{Project: project})
	for {
		pair, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("gcp: aggregated list disks (%s): %w", project, err)
		}
		for _, d := range pair.Value.GetDisks() {
			idx[lastSegment(d.GetZone())+"/"+d.GetName()] = d
		}
	}
	return idx, nil
}

func (p *Provider) toInstance(ctx context.Context, project, zone, region string, vm *computepb.Instance, diskIdx map[string]*computepb.Disk) model.Instance {
	mtName := lastSegment(vm.GetMachineType())
	spec := p.machineSpec(ctx, project, zone, mtName)

	in := model.Instance{
		ID:                strconv.FormatUint(vm.GetId(), 10),
		Name:              vm.GetName(),
		Provider:          "gcp",
		Project:           project,
		Region:            region,
		Zone:              zone,
		MachineType:       mtName,
		ProvisioningModel: provisioningModel(vm),
		VCPU:              spec.vCPU,
		MemGB:             spec.memGB,
		NICGbps:           nicGbps(mtName, spec.vCPU),
		Labels:            vm.GetLabels(),
	}
	for _, ad := range vm.GetDisks() {
		disk := model.Disk{
			Name:   ad.GetDeviceName(),
			SizeGB: float64(ad.GetDiskSizeGb()),
		}
		// Resolve the backing PD resource for its type + provisioned IOPS so we
		// can compute a real IOPS ceiling (the attached-disk record lacks both).
		if src := lastSegment(ad.GetSource()); src != "" {
			if pd, ok := diskIdx[zone+"/"+src]; ok {
				disk.Type = lastSegment(pd.GetType())
				if disk.SizeGB == 0 {
					disk.SizeGB = float64(pd.GetSizeGb())
				}
				disk.IOPSCap = diskIOPSCeiling(disk.Type, disk.SizeGB, float64(pd.GetProvisionedIops()))
			}
		}
		in.Disks = append(in.Disks, disk)
	}
	return in
}

// machineSpec resolves vCPU/RAM for a machine type, caching by zone/name.
func (p *Provider) machineSpec(ctx context.Context, project, zone, name string) machineSpec {
	key := zone + "/" + name
	if s, ok := p.mtCache[key]; ok {
		return s
	}
	var s machineSpec
	mt, err := p.machines.Get(ctx, &computepb.GetMachineTypeRequest{
		Project:     project,
		Zone:        zone,
		MachineType: name,
	})
	if err == nil {
		s = machineSpec{
			vCPU:  float64(mt.GetGuestCpus()),
			memGB: float64(mt.GetMemoryMb()) / 1024.0,
		}
	}
	p.mtCache[key] = s
	return s
}

// provisioningModel classifies a VM's scheduling as spot or on-demand.
// Preemptible covers legacy preemptible VMs that predate the SPOT model.
func provisioningModel(vm *computepb.Instance) model.ProvisioningModel {
	s := vm.GetScheduling()
	if s.GetProvisioningModel() == computepb.Scheduling_SPOT.String() || s.GetPreemptible() {
		return model.ProvisioningSpot
	}
	return model.ProvisioningOnDemand
}

// ListNodePools enumerates GKE nodepools (inventory only).
func (p *Provider) ListNodePools(ctx context.Context) ([]model.NodePool, error) {
	var out []model.NodePool
	for _, project := range p.projects {
		resp, err := p.clusters.ListClusters(ctx, &containerpb.ListClustersRequest{
			Parent: fmt.Sprintf("projects/%s/locations/-", project),
		})
		if err != nil {
			// A project without GKE has the container API disabled; that
			// means "no clusters", not a failed scan.
			if strings.Contains(err.Error(), "SERVICE_DISABLED") {
				fmt.Fprintf(os.Stderr, "warning: gcp: Kubernetes Engine API disabled in %s; skipping GKE inventory\n", project)
				continue
			}
			return nil, fmt.Errorf("gcp: list clusters (%s): %w", project, err)
		}
		for _, c := range resp.GetClusters() {
			region := regionOfZone(c.GetLocation())
			if !p.regionAllowed(region) {
				continue
			}
			for _, np := range c.GetNodePools() {
				out = append(out, model.NodePool{
					Cluster:     c.GetName(),
					Name:        np.GetName(),
					Provider:    "gcp",
					Region:      c.GetLocation(),
					MachineType: np.GetConfig().GetMachineType(),
					NodeCount:   int(np.GetInitialNodeCount()),
				})
			}
		}
	}
	return out, nil
}

func (p *Provider) regionAllowed(region string) bool {
	if len(p.regions) == 0 {
		return true
	}
	return p.regions[region]
}

// lastSegment returns the final path/URL segment (after the last '/').
func lastSegment(s string) string {
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}

// regionOfZone turns "us-central1-a" into "us-central1"; a region string is
// returned unchanged.
func regionOfZone(z string) string {
	if i := strings.LastIndex(z, "-"); i >= 0 {
		// only strip if the suffix looks like a zone letter
		if len(z)-i == 2 {
			return z[:i]
		}
	}
	return z
}
