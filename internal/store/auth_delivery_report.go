package store

import (
	"context"
	"time"

	"tdlibgo/internal/domain"
)

type AuthDeliveryReportStore interface {
	CreateAuthDeliveryReport(ctx context.Context, report domain.AuthDeliveryReport) (domain.AuthDeliveryReport, bool, error)
	DeleteExpiredAuthDeliveryReports(ctx context.Context, olderThan time.Time, limit int) (int, error)
}
