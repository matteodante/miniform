package jobs

import (
	"io"
)

func discardBody(body io.Reader) error {
	_, err := io.Copy(io.Discard, io.LimitReader(body, 64<<10))
	return err
}
