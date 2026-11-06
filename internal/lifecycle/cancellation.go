package lifecycle

import "context"

// rootCancellationOnly must not discard an independent failure joined with a
// cancellation (for example, an interrupted script plus a failed journal save).
func rootCancellationOnly(ctx context.Context, err error) bool {
	root := ctx.Err()
	if root == nil || err == nil {
		return false
	}
	if err == root {
		return true
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		causes := joined.Unwrap()
		if len(causes) == 0 {
			return false
		}
		for _, cause := range causes {
			if !rootCancellationOnly(ctx, cause) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return rootCancellationOnly(ctx, wrapped.Unwrap())
	}
	return false
}
