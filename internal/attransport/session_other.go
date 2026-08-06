//go:build !linux

package attransport

import "errors"

type unsupportedOpener struct{}

func newPlatformOpener() Opener { return unsupportedOpener{} }

func (unsupportedOpener) Open(string) (Session, error) {
	return nil, &OpenError{Kind: OpenUnsupported, cause: errors.New("AT transport is supported only on Linux")}
}
