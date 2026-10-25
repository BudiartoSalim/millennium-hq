package usecase

import (
	"errors"
	"testing"

	"millennium/internal/application/port"
	"millennium/internal/domain"
)

func TestLoadConfig(t *testing.T) {
	valid := domain.Config{Languages: []domain.Language{domain.LanguageGo}}
	loadErr := errors.New("disk on fire")
	tests := []struct {
		name        string
		source      fakeSource
		repo        *fakeRepo
		wantEnabled bool
		wantErr     error
	}{
		{"config present", fakeSource{cfg: valid}, &fakeRepo{}, true, nil},
		{"never configured", fakeSource{err: port.ErrConfigNotFound}, &fakeRepo{tracked: false}, false, nil},
		{"removed from working tree", fakeSource{err: port.ErrConfigNotFound}, &fakeRepo{tracked: true}, false, ErrConfigRemoved},
		{"load fails", fakeSource{err: loadErr}, &fakeRepo{}, false, loadErr},
		{"tracked check fails", fakeSource{err: port.ErrConfigNotFound}, &fakeRepo{trackedErr: loadErr}, false, loadErr},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, enabled, err := NewLoadConfig(tt.source, tt.repo).Execute(t.Context())
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if enabled != tt.wantEnabled {
				t.Fatalf("enabled = %v, want %v", enabled, tt.wantEnabled)
			}
			if enabled && !cfg.Has(domain.LanguageGo) {
				t.Fatalf("config not returned: %+v", cfg)
			}
		})
	}
}
