package aws

import (
	"context"
	"fmt"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"

	"github.com/footprintai/telescope/internal/model"
)

// ListNodePools enumerates EKS managed nodegroups (inventory only).
func (p *Provider) ListNodePools(ctx context.Context) ([]model.NodePool, error) {
	var out []model.NodePool
	for _, region := range p.regions {
		cli := eks.NewFromConfig(p.base, func(o *eks.Options) { o.Region = region })

		clusters, err := listClusters(ctx, cli)
		if err != nil {
			return nil, fmt.Errorf("aws: list clusters (%s): %w", region, err)
		}
		for _, cluster := range clusters {
			groups, err := listNodegroups(ctx, cli, cluster)
			if err != nil {
				return nil, fmt.Errorf("aws: list nodegroups (%s/%s): %w", region, cluster, err)
			}
			for _, ng := range groups {
				desc, err := cli.DescribeNodegroup(ctx, &eks.DescribeNodegroupInput{
					ClusterName:   awssdk.String(cluster),
					NodegroupName: awssdk.String(ng),
				})
				if err != nil {
					return nil, fmt.Errorf("aws: describe nodegroup (%s/%s): %w", cluster, ng, err)
				}
				np := model.NodePool{
					Cluster:  cluster,
					Name:     ng,
					Provider: "aws",
					Region:   region,
				}
				if n := desc.Nodegroup; n != nil {
					if len(n.InstanceTypes) > 0 {
						np.MachineType = n.InstanceTypes[0]
					}
					if n.ScalingConfig != nil {
						np.NodeCount = int(awssdk.ToInt32(n.ScalingConfig.DesiredSize))
					}
				}
				out = append(out, np)
			}
		}
	}
	return out, nil
}

func listClusters(ctx context.Context, cli *eks.Client) ([]string, error) {
	var names []string
	pager := eks.NewListClustersPaginator(cli, &eks.ListClustersInput{})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		names = append(names, page.Clusters...)
	}
	return names, nil
}

func listNodegroups(ctx context.Context, cli *eks.Client, cluster string) ([]string, error) {
	var names []string
	pager := eks.NewListNodegroupsPaginator(cli, &eks.ListNodegroupsInput{
		ClusterName: awssdk.String(cluster),
	})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		names = append(names, page.Nodegroups...)
	}
	return names, nil
}
