//go:build linux && amd64

package auxiliary

import (
	"path/filepath"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/helper"
)

func Register(service *app.Service, configDirectory string) {
	backend, ok := service.Provider.(domain.ColdStateInspector)
	if !ok {
		return
	}
	s := &Service{Backend: backend, Helper: helper.Client{KeyPath: filepath.Join(configDirectory, "helper-key.pem")}}
	s.Register(service)
}
