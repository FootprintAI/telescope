package aws

import (
	"testing"
	"time"

	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

func TestLaunchedAt(t *testing.T) {
	launch := time.Date(2024, 3, 10, 8, 0, 0, 0, time.UTC)

	if got := launchedAt(ec2types.Instance{LaunchTime: &launch}); !got.Equal(launch) {
		t.Errorf("launchedAt with LaunchTime = %v, want %v", got, launch)
	}
	if got := launchedAt(ec2types.Instance{}); !got.IsZero() {
		t.Errorf("launchedAt with nil LaunchTime = %v, want zero", got)
	}
}
