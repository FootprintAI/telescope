package aws

import (
	"strconv"
	"strings"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// networkGbps derives an egress ceiling (Gbit/s) for an instance type. It
// prefers the structured peak/baseline bandwidth fields, falling back to
// parsing the legacy NetworkPerformance string ("Up to 10 Gigabit", "High", …).
func networkGbps(ni *ec2types.NetworkInfo) float64 {
	if ni == nil {
		return 0
	}
	for _, c := range ni.NetworkCards {
		if c.PeakBandwidthInGbps != nil && *c.PeakBandwidthInGbps > 0 {
			return *c.PeakBandwidthInGbps
		}
		if c.BaselineBandwidthInGbps != nil && *c.BaselineBandwidthInGbps > 0 {
			return *c.BaselineBandwidthInGbps
		}
	}
	return parseNetworkPerformance(awssdk.ToString(ni.NetworkPerformance))
}

// parseNetworkPerformance interprets strings like "Up to 25 Gigabit",
// "10 Gigabit", or the legacy qualitative tiers.
func parseNetworkPerformance(s string) float64 {
	if s == "" {
		return 0
	}
	fields := strings.Fields(s)
	for _, f := range fields {
		if v, err := strconv.ParseFloat(f, 64); err == nil {
			return v // already in Gigabit
		}
	}
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "high":
		return 5
	case "moderate":
		return 1
	case "low":
		return 0.5
	case "very low":
		return 0.1
	}
	return 0
}

// ebsIOPSCeiling returns an IOPS ceiling for an EBS volume. Provisioned types
// (io1/io2/gp3) report Iops directly; gp2 scales at 3 IOPS/GB (min 100, cap
// 16000). Throughput types (st1/sc1) and unknowns return 0 (dimension skipped).
func ebsIOPSCeiling(volType string, sizeGB, iops float64) float64 {
	switch strings.ToLower(volType) {
	case "io1", "io2", "gp3":
		return iops
	case "gp2":
		v := sizeGB * 3
		if v < 100 {
			v = 100
		}
		if v > 16000 {
			v = 16000
		}
		return v
	default:
		return 0
	}
}
