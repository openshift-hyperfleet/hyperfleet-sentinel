package logger

import (
	"context"

	"github.com/segmentio/ksuid"
)

type OperationIDKey string

const OpIDKey OperationIDKey = "op_id"

// TransactionIDKey is the typed context key for transaction ID
type TransactionIDKey string

const TxIDKey TransactionIDKey = "tx_id"

func WithOpID(ctx context.Context) context.Context {
	if ctx.Value(OpIDKey) != nil {
		return ctx
	}
	opID := ksuid.New().String()
	return context.WithValue(ctx, OpIDKey, opID)
}

// GetOperationID get operationID of the context
func GetOperationID(ctx context.Context) string {
	if opID, ok := ctx.Value(OpIDKey).(string); ok {
		return opID
	}
	return ""
}
