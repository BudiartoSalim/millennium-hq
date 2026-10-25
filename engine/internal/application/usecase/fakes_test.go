package usecase

import (
	"context"

	"millennium/internal/application/port"
	"millennium/internal/domain"
)

type fakeSource struct {
	cfg domain.Config
	err error
}

func (f fakeSource) Load() (domain.Config, error) { return f.cfg, f.err }

type fakeRepo struct {
	changes    []domain.ChangedFile
	changesErr error
	tracked    bool
	trackedErr error
	gotBase    string
}

func (f *fakeRepo) Changes(_ context.Context, base string) ([]domain.ChangedFile, error) {
	f.gotBase = base
	return f.changes, f.changesErr
}

func (f *fakeRepo) Tracked(context.Context, string, string) (bool, error) {
	return f.tracked, f.trackedErr
}

type fakeGate struct {
	name     string
	findings []domain.Finding
	err      error
	panicked bool
	got      port.GateInput
}

func (f *fakeGate) Name() string { return f.name }

func (f *fakeGate) Run(_ context.Context, in port.GateInput) ([]domain.Finding, error) {
	f.got = in
	if f.panicked {
		panic("boom")
	}
	return f.findings, f.err
}
