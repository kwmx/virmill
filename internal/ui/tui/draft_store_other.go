//go:build !linux

package tui

import "errors"

var (
	ErrDraftConflict = errors.New("this draft changed in another Virmill window; reload it before saving")
	ErrDraftBusy     = errors.New("another Virmill window is saving drafts; try again")
)

type DraftStore struct{}

func NewDraftStore() (*DraftStore, error) { return nil, errors.New("private TUI drafts require Linux") }
func (*DraftStore) Close() error          { return nil }
func (*DraftStore) Load(string, any) (string, error) {
	return "", errors.New("private TUI drafts require Linux")
}
func (*DraftStore) Save(string, string, any) (string, error) {
	return "", errors.New("private TUI drafts require Linux")
}
func (*DraftStore) Delete(string, string) error {
	return errors.New("private TUI drafts require Linux")
}
