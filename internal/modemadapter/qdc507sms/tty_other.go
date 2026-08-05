//go:build !linux

package qdc507sms

func openSerialSession(string) (serialSession, error) {
	return nil, ErrTTYUnsupported
}
