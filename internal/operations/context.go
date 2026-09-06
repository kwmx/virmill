package operations

import "context"

type operationContextKey struct{}

// OperationID identifies a durably accepted job at an execution boundary.
func OperationID(ctx context.Context) string {
	id, _ := ctx.Value(operationContextKey{}).(string)
	return id
}
