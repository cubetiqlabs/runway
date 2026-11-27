//go:build !linux && !darwin && !windows

package agent

import "github.com/cubetiqlabs/runway/internal/domain"

func (m *MetricsCollector) fillPlatformMetrics(_ *domain.AgentMetricsSnapshot) {}
