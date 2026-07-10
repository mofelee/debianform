package engine

type redactedPayloadError struct {
	err error
}

func (e redactedPayloadError) Error() string {
	return "redacted payload command failed: <redacted>"
}

func (e redactedPayloadError) Unwrap() error {
	return e.err
}

func redactPayloadError(err error) error {
	if err == nil {
		return nil
	}
	return redactedPayloadError{err: err}
}
