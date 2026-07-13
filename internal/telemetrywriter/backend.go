package telemetrywriter

import (
	"context"

	"github.com/makinje/aero-arc-relay/internal/telemetrynormalize"
)

// Backend persists normalized telemetry records. Implementations must be safe
// for concurrent calls when WriterConfig.Workers is greater than one.
type Backend interface {
	WriteBatch(context.Context, []telemetrynormalize.Record) error
	Close(context.Context) error
}
