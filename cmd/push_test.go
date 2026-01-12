// cmd/push_test.go
package cmd

import "testing"

func TestParseSquashRange(t *testing.T) {
	tests := []struct {
		input     string
		wantStart int
		wantEnd   int
		wantErr   bool
	}{
		{"2-4", 2, 4, false},
		{"1-10", 1, 10, false},
		{"5-3", 0, 0, true},  // start >= end
		{"abc", 0, 0, true},  // invalid format
		{"2-", 0, 0, true},   // missing end
		{"-4", 0, 0, true},   // missing start
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			start, end, err := parseSquashRange(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if start != tt.wantStart {
				t.Errorf("start = %d, want %d", start, tt.wantStart)
			}
			if end != tt.wantEnd {
				t.Errorf("end = %d, want %d", end, tt.wantEnd)
			}
		})
	}
}

func TestPushCommand(t *testing.T) {
	cmd := pushCmd

	if cmd.Use != "push" {
		t.Errorf("expected Use 'push', got %s", cmd.Use)
	}

	// Verify flags exist
	previewFlag := cmd.Flags().Lookup("preview")
	if previewFlag == nil {
		t.Error("expected --preview flag")
	}

	squashFlag := cmd.Flags().Lookup("squash")
	if squashFlag == nil {
		t.Error("expected --squash flag")
	}
}
