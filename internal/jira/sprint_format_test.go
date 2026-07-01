package jira

import (
	"encoding/json"
	"testing"
)

func TestFormatSprintField(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "empty",
			raw:  `null`,
			want: "",
		},
		{
			name: "modern object array, single active",
			raw:  `[{"id":153,"name":"Sprint 153","state":"active"}]`,
			want: "Sprint 153 (active)",
		},
		{
			name: "modern object array, closed then active",
			raw:  `[{"id":152,"name":"Sprint 152","state":"closed"},{"id":153,"name":"Sprint 153","state":"active"}]`,
			want: "Sprint 152, Sprint 153 (active)",
		},
		{
			name: "legacy greenhopper toString",
			raw:  `["com.atlassian.greenhopper.service.sprint.Sprint@1[id=28037,rapidViewId=45,state=CLOSED,name=Sprint 153,startDate=2024-01-01,endDate=2024-01-14]"]`,
			want: "Sprint 153",
		},
		{
			name: "unnamed falls back to id",
			raw:  `[{"id":99,"state":"future"}]`,
			want: "sprint 99",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FormatSprintField(json.RawMessage(tc.raw))
			if got != tc.want {
				t.Errorf("FormatSprintField(%s) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}
