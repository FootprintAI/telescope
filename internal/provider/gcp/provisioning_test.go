package gcp

import (
	"testing"

	computepb "cloud.google.com/go/compute/apiv1/computepb"
	"google.golang.org/protobuf/proto"

	"github.com/footprintai/telescope/internal/model"
)

func TestProvisioningModel(t *testing.T) {
	cases := []struct {
		name string
		vm   *computepb.Instance
		want model.ProvisioningModel
	}{
		{
			name: "spot",
			vm: &computepb.Instance{Scheduling: &computepb.Scheduling{
				ProvisioningModel: proto.String("SPOT"),
			}},
			want: model.ProvisioningSpot,
		},
		{
			name: "legacy preemptible",
			vm: &computepb.Instance{Scheduling: &computepb.Scheduling{
				Preemptible: proto.Bool(true),
			}},
			want: model.ProvisioningSpot,
		},
		{
			name: "standard",
			vm: &computepb.Instance{Scheduling: &computepb.Scheduling{
				ProvisioningModel: proto.String("STANDARD"),
			}},
			want: model.ProvisioningOnDemand,
		},
		{
			name: "no scheduling block",
			vm:   &computepb.Instance{},
			want: model.ProvisioningOnDemand,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := provisioningModel(tc.vm); got != tc.want {
				t.Fatalf("provisioningModel = %q, want %q", got, tc.want)
			}
		})
	}
}
