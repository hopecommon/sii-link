package gateway

import (
	"errors"
	"os"
)

func lock(string) (*os.File, error) {
	return nil, errors.New("gateway supervision requires macOS or Linux")
}
