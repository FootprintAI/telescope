// Package aws implements a read-only Provider over Amazon Web Services: EC2
// instances, EKS nodegroups (inventory), and CloudWatch metrics.
//
// Credentials come from cfg.CredentialsFile (a shared-credentials file) when
// set, otherwise the default AWS chain (env, shared config, SSO, instance role).
// AWS is region-scoped: use --regions to limit the scan, else all enabled
// regions are queried. The --projects flag is not used for AWS (the account is
// determined by the credentials).
package aws

import (
	"context"
	"fmt"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/footprintai/telescope/internal/model"
	"github.com/footprintai/telescope/internal/provider"
)

// Provider is an AWS read-only backend.
type Provider struct {
	base    awssdk.Config
	regions []string

	// instanceType spec cache (specs are identical across regions).
	specCache map[string]instanceSpec
}

type instanceSpec struct {
	vCPU    float64
	memGB   float64
	nicGbps float64
}

// New builds an AWS provider and resolves the region list.
func New(ctx context.Context, cfg provider.Config) (*Provider, error) {
	var opts []func(*config.LoadOptions) error
	if cfg.CredentialsFile != "" {
		opts = append(opts, config.WithSharedCredentialsFiles([]string{cfg.CredentialsFile}))
	}
	base, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("aws: load config: %w", err)
	}
	if base.Region == "" {
		base.Region = "us-east-1" // needed for the DescribeRegions bootstrap call
	}

	regions := cfg.Regions
	if len(regions) == 0 {
		regions, err = enabledRegions(ctx, base)
		if err != nil {
			return nil, err
		}
	}
	return &Provider{base: base, regions: regions, specCache: map[string]instanceSpec{}}, nil
}

// Name implements provider.Provider.
func (p *Provider) Name() string { return "aws" }

// enabledRegions lists the account's opted-in regions.
func enabledRegions(ctx context.Context, base awssdk.Config) ([]string, error) {
	cli := ec2.NewFromConfig(base)
	out, err := cli.DescribeRegions(ctx, &ec2.DescribeRegionsInput{})
	if err != nil {
		return nil, fmt.Errorf("aws: describe regions: %w", err)
	}
	var rs []string
	for _, r := range out.Regions {
		rs = append(rs, awssdk.ToString(r.RegionName))
	}
	return rs, nil
}

func (p *Provider) ec2(region string) *ec2.Client {
	return ec2.NewFromConfig(p.base, func(o *ec2.Options) { o.Region = region })
}

// ListInstances enumerates EC2 instances across the configured regions.
func (p *Provider) ListInstances(ctx context.Context) ([]model.Instance, error) {
	var out []model.Instance
	for _, region := range p.regions {
		cli := p.ec2(region)
		volIdx, err := p.loadVolumeIndex(ctx, cli, region)
		if err != nil {
			return nil, err
		}
		pager := ec2.NewDescribeInstancesPaginator(cli, &ec2.DescribeInstancesInput{})
		for pager.HasMorePages() {
			page, err := pager.NextPage(ctx)
			if err != nil {
				return nil, fmt.Errorf("aws: describe instances (%s): %w", region, err)
			}
			for _, res := range page.Reservations {
				for _, vm := range res.Instances {
					if vm.State != nil && vm.State.Name == ec2types.InstanceStateNameTerminated {
						continue
					}
					out = append(out, p.toInstance(ctx, cli, region, vm, volIdx))
				}
			}
		}
	}
	return out, nil
}

func (p *Provider) toInstance(ctx context.Context, cli *ec2.Client, region string, vm ec2types.Instance, volIdx map[string]ec2types.Volume) model.Instance {
	itype := string(vm.InstanceType)
	spec := p.instanceSpec(ctx, cli, itype)

	in := model.Instance{
		ID:          awssdk.ToString(vm.InstanceId),
		Name:        tagValue(vm.Tags, "Name"),
		Provider:    "aws",
		Project:     "", // account id is available via STS if needed
		Region:      region,
		Zone:        placementZone(vm),
		MachineType: itype,
		VCPU:        spec.vCPU,
		MemGB:       spec.memGB,
		NICGbps:     spec.nicGbps,
		Labels:      tagMap(vm.Tags),
	}
	if in.Name == "" {
		in.Name = in.ID
	}
	for _, bdm := range vm.BlockDeviceMappings {
		if bdm.Ebs == nil {
			continue
		}
		volID := awssdk.ToString(bdm.Ebs.VolumeId)
		d := model.Disk{Name: awssdk.ToString(bdm.DeviceName)}
		if v, ok := volIdx[volID]; ok {
			d.Type = string(v.VolumeType)
			d.SizeGB = float64(awssdk.ToInt32(v.Size))
			d.IOPSCap = ebsIOPSCeiling(d.Type, d.SizeGB, float64(awssdk.ToInt32(v.Iops)))
		}
		in.Disks = append(in.Disks, d)
	}
	return in
}

// instanceSpec resolves vCPU/RAM/NIC for an instance type, caching by type.
func (p *Provider) instanceSpec(ctx context.Context, cli *ec2.Client, itype string) instanceSpec {
	if s, ok := p.specCache[itype]; ok {
		return s
	}
	var s instanceSpec
	out, err := cli.DescribeInstanceTypes(ctx, &ec2.DescribeInstanceTypesInput{
		InstanceTypes: []ec2types.InstanceType{ec2types.InstanceType(itype)},
	})
	if err == nil && len(out.InstanceTypes) > 0 {
		it := out.InstanceTypes[0]
		if it.VCpuInfo != nil {
			s.vCPU = float64(awssdk.ToInt32(it.VCpuInfo.DefaultVCpus))
		}
		if it.MemoryInfo != nil {
			s.memGB = float64(awssdk.ToInt64(it.MemoryInfo.SizeInMiB)) / 1024.0
		}
		s.nicGbps = networkGbps(it.NetworkInfo)
	}
	p.specCache[itype] = s
	return s
}

// loadVolumeIndex fetches EBS volumes in a region once, keyed by volume-id.
func (p *Provider) loadVolumeIndex(ctx context.Context, cli *ec2.Client, region string) (map[string]ec2types.Volume, error) {
	idx := map[string]ec2types.Volume{}
	pager := ec2.NewDescribeVolumesPaginator(cli, &ec2.DescribeVolumesInput{})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("aws: describe volumes (%s): %w", region, err)
		}
		for _, v := range page.Volumes {
			idx[awssdk.ToString(v.VolumeId)] = v
		}
	}
	return idx, nil
}

func placementZone(vm ec2types.Instance) string {
	if vm.Placement != nil {
		return awssdk.ToString(vm.Placement.AvailabilityZone)
	}
	return ""
}

func tagValue(tags []ec2types.Tag, key string) string {
	for _, t := range tags {
		if awssdk.ToString(t.Key) == key {
			return awssdk.ToString(t.Value)
		}
	}
	return ""
}

func tagMap(tags []ec2types.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	m := make(map[string]string, len(tags))
	for _, t := range tags {
		m[awssdk.ToString(t.Key)] = awssdk.ToString(t.Value)
	}
	return m
}
