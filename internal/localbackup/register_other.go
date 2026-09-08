//go:build !linux || !amd64

package localbackup

import (
	"context"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
)

func Register(a *app.Service, data, cache string) {
	if a.Extensions == nil {
		a.Extensions = map[string]func(context.Context, uint32, app.Request) (any, error){}
	}
	for _, method := range []string{"backup.repository.init", "backup.repository.check", "backup.create", "backup.restore", "backup.result"} {
		a.Extensions[method] = func(context.Context, uint32, app.Request) (any, error) {
			return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "local backup operations require Linux amd64")
		}
	}
}
