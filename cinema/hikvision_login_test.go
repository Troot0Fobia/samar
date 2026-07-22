package cinema

import "testing"

// TestHikSessionUserCheckIsLocked covers the sessionLogin XML bodies captured
// from real rejecting Hikvision devices (hikvision_incorrect_data.pcapng):
// every single one reported lockStatus="unlock" (not locked) even on repeated
// wrong-password rejections. A naive strings.Contains(lockStatus, "lock")
// match is wrong because "unlock" contains "lock" as a substring — it was
// misreporting every plain wrong password as "account locked".
func TestHikSessionUserCheckIsLocked(t *testing.T) {
	cases := []struct {
		lockStatus string
		want       bool
	}{
		{"unlock", false}, // captured verbatim from ~55 rejecting devices
		{"Unlock", false},
		{"UNLOCK", false},
		{"", false},
		{"locked", true},
		{"lock", true},
		{"Locked", true},
	}
	for _, tc := range cases {
		check := hikSessionUserCheck{LockStatus: tc.lockStatus}
		if got := check.isLocked(); got != tc.want {
			t.Fatalf("isLocked(%q) = %v, want %v", tc.lockStatus, got, tc.want)
		}
	}
}
