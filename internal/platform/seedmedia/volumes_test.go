package seedmedia

import "testing"

func TestMediaLabelsAndFilesystemCompatibility(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		label, filesystem, expectedLabel string
		want                             bool
	}{
		{"CIDATA", "CDFS", "cidata", true},
		{"CONFIG-2", "fat32", "config-2", true},
		{"config-2", "ISO9660", "config-2", true},
		{"config-2", "FAT", "config-2", true},
		{"CIDATA", "FAT32", "config-2", false},
		{"config-2", "FAT32", "cidata", false},
		{"config-2", "NTFS", "config-2", false},
		{"config-2", "exFAT", "config-2", false},
		{"data", "CDFS", "config-2", false},
	} {
		if got := matchesVolume(tc.label, tc.filesystem, tc.expectedLabel); got != tc.want {
			t.Errorf("matchesVolume(%q,%q,%q)=%t want %t", tc.label, tc.filesystem, tc.expectedLabel, got, tc.want)
		}
	}
}
