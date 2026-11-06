package nocloud

import (
	"context"

	"github.com/randomtoy/wininit/internal/platform/seedmedia"
)

// FindVolumes discovers already-mounted NoCloud CIDATA media.
func FindVolumes(ctx context.Context) ([]string, error) { return seedmedia.FindVolumes(ctx, "cidata") }
