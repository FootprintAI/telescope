// Package provider defines the cloud-backend interface and a small registry.
package provider

import (
	"context"

	"github.com/footprintai/telescope/internal/model"
)

// Provider is a read-only cloud backend (GCP, AWS, mock, ...).
type Provider interface {
	// Name is the short provider id, e.g. "gcp".
	Name() string
	// ListInstances enumerates VMs (GCE / EC2).
	ListInstances(ctx context.Context) ([]model.Instance, error)
	// ListNodePools enumerates managed-k8s nodepools (GKE / EKS).
	ListNodePools(ctx context.Context) ([]model.NodePool, error)
	// FetchMetrics fills each instance's Metrics from the monitoring backend
	// over the given window. It mutates the passed slice in place and also
	// returns it for convenience.
	FetchMetrics(ctx context.Context, insts []model.Instance, w model.Window) ([]model.Instance, error)
}

// Config is the common configuration handed to a provider factory.
type Config struct {
	Projects        []string // GCP projects / AWS accounts
	Regions         []string // empty = all
	CredentialsFile string   // path to SA JSON / AWS shared creds (optional)
}
