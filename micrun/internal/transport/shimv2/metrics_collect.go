package shim

import (
	"context"
	"fmt"

	cntr "micrun/internal/domain/container"
	er "micrun/internal/support/errors"
	log "micrun/internal/support/logger"
	"micrun/internal/support/validation"

	cgroupsv1 "github.com/containerd/cgroups/stats/v1"
	ptypes "github.com/containerd/containerd/protobuf/types"
	typeurl "github.com/containerd/typeurl/v2"
	"github.com/gogo/protobuf/proto"
)

type metricsSource struct {
	sandbox cntr.SandboxTraits
	shimPID uint32
}

type metricsRuntime interface {
	currentSandbox() (cntr.SandboxTraits, bool)
}

func metricsSourceFromRuntime(s metricsRuntime, shimPID uint32) metricsSource {
	if s == nil {
		return metricsSource{}
	}
	sandbox, _ := s.currentSandbox()
	return metricsSource{
		sandbox: sandbox,
		shimPID: shimPID,
	}
}

// marshalMetrics collects container stats and marshals them into a protobuf Any type.
func marshalMetrics(ctx context.Context, source metricsSource, cid string) (*ptypes.Any, error) {
	if validation.IsNil(source.sandbox) {
		log.Debugf("Sandbox is nil, cannot get stats for container %s", cid)
		return nil, er.SandboxNotFound
	}
	stats, err := source.sandbox.StatsContainer(ctx, cid)
	if err != nil {
		return nil, err
	}

	isCgroupV1, err := cgroupV1()
	if err != nil {
		log.Debugf("failed to determine cgroup version: %v", err)
		isCgroupV1 = true
	}

	var metrics proto.Message
	var (
		cpuUsageUsec   uint64
		memUsageBytes  uint64
		memLimitBytes  uint64
		memFailcnt     uint64
		memMaxBytes    uint64
		resourceExists bool
	)

	if stats.ResourceStats != nil {
		resourceExists = true
		cpuUsageUsec = stats.ResourceStats.CPUStats.TotalUsage
		memUsageBytes = stats.ResourceStats.MemoryStats.Usage.Usage
		memLimitBytes = stats.ResourceStats.MemoryStats.Usage.Limit
		memFailcnt = stats.ResourceStats.MemoryStats.Usage.Failcnt
		memMaxBytes = stats.ResourceStats.MemoryStats.Usage.MaxEver
	}

	if isCgroupV1 {
		metrics = statsToMetricsV1(&stats)
	} else {
		metrics = statsToMetricsV2(&stats, source.shimPID)
	}
	if metrics == nil {
		return nil, fmt.Errorf("metrics is nil for container %s", cid)
	}

	typeAny, err := typeurl.MarshalAny(metrics)
	if err != nil {
		return nil, err
	}

	log.Debugf(
		"marshalMetrics: container=%s cgroup_v1=%t resources=%t cpu_usage_usec=%d memory_usage_bytes=%d memory_limit_bytes=%d memory_failcnt=%d memory_max_bytes=%d type=%s",
		cid,
		isCgroupV1,
		resourceExists,
		cpuUsageUsec,
		memUsageBytes,
		memLimitBytes,
		memFailcnt,
		memMaxBytes,
		typeAny.GetTypeUrl(),
	)

	return &ptypes.Any{
		TypeUrl: typeAny.GetTypeUrl(),
		Value:   typeAny.GetValue(),
	}, nil
}

// emptyMetricsV1 returns empty metrics for when stats collection fails.
func emptyMetricsV1() *ptypes.Any {
	m := statsToMetricsV1(nil)
	typeAny, err := typeurl.MarshalAny(m)
	if err != nil {
		return &ptypes.Any{}
	}
	return &ptypes.Any{
		TypeUrl: typeAny.GetTypeUrl(),
		Value:   typeAny.GetValue(),
	}
}

// compile-time guard: conversion target for empty metrics remains cgroup v1.
var _ = (*cgroupsv1.Metrics)(nil)
var _ = (*cntr.ContainerStats)(nil)
