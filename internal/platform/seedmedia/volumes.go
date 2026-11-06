// Package seedmedia discovers supported mounted local initialization media.
package seedmedia

import "strings"

func matchesVolume(label, filesystem, expectedLabel string) bool {
	if !strings.EqualFold(label, expectedLabel) {
		return false
	}
	switch strings.ToUpper(filesystem) {
	case "FAT", "FAT32", "CDFS", "ISO9660":
		return true
	default:
		return false
	}
}
