package agents

import (
	"bytes"
	"strings"
	"testing"

	"github.com/rmedranollamas/x-agent/internal/config"
)

func TestGenerateReport_DeduplicatesHeaderCounts(t *testing.T) {
	var outBuf bytes.Buffer
	agent := NewInsightsAgent(nil, nil, &config.Config{Environment: "development"}, InsightsOptions{}, &outBuf)

	report := agent.generateReport(insightsReportData{
		FollowersCount: 10,
		FollowingCount: 5,
		NewIDs:         []int64{101, 101, 102},
		LostIDs:        []int64{201, 201},
		NewUserMap: map[int64]string{
			101: "alice",
			102: "(Deactivated)",
		},
		LostUserMap: map[int64]string{
			201: "(Suspended)",
		},
	})

	if !strings.Contains(report, "New (2):") {
		t.Errorf("expected deduplicated 'New (2):' header, got:\n%s", report)
	}
	if !strings.Contains(report, "Lost (1):") {
		t.Errorf("expected deduplicated 'Lost (1):' header, got:\n%s", report)
	}
}
