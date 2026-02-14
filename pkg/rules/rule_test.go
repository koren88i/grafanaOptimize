package rules

import "testing"

func TestSeverityWeight(t *testing.T) {
	tests := []struct {
		severity Severity
		want     int
	}{
		{Critical, 15},
		{High, 10},
		{Medium, 5},
		{Low, 2},
	}
	for _, tt := range tests {
		got := SeverityWeight(tt.severity)
		if got != tt.want {
			t.Errorf("SeverityWeight(%s) = %d, want %d", tt.severity, got, tt.want)
		}
	}
}

func TestSeverityString(t *testing.T) {
	tests := []struct {
		severity Severity
		want     string
	}{
		{Critical, "Critical"},
		{High, "High"},
		{Medium, "Medium"},
		{Low, "Low"},
	}
	for _, tt := range tests {
		got := tt.severity.String()
		if got != tt.want {
			t.Errorf("Severity(%d).String() = %q, want %q", int(tt.severity), got, tt.want)
		}
	}
}

func TestComputeScore(t *testing.T) {
	tests := []struct {
		name     string
		findings []Finding
		want     int
	}{
		{
			name:     "no findings = perfect score",
			findings: nil,
			want:     100,
		},
		{
			name: "one critical finding",
			findings: []Finding{
				{Severity: Critical},
			},
			want: 85,
		},
		{
			name: "mixed severities",
			findings: []Finding{
				{Severity: Critical}, // -15
				{Severity: High},     // -10
				{Severity: Medium},   // -5
				{Severity: Low},      // -2
			},
			want: 68,
		},
		{
			name: "score clamps to zero",
			findings: []Finding{
				{Severity: Critical}, // -15
				{Severity: Critical}, // -15
				{Severity: Critical}, // -15
				{Severity: Critical}, // -15
				{Severity: Critical}, // -15
				{Severity: Critical}, // -15
				{Severity: Critical}, // -15 = -105 → 0
			},
			want: 0,
		},
		{
			name: "many medium findings",
			findings: []Finding{
				{Severity: Medium}, // -5
				{Severity: Medium}, // -5
				{Severity: Medium}, // -5
				{Severity: Medium}, // -5
			},
			want: 80,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeScore(tt.findings)
			if got != tt.want {
				t.Errorf("ComputeScore() = %d, want %d", got, tt.want)
			}
		})
	}
}
