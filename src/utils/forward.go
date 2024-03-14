package utils

import (
	"io"

	"github.com/bbk47/toolbox"
)

func Forward(left, right io.ReadWriteCloser, label string, logger *toolbox.Logger) {
	defer left.Close()
	defer right.Close()
	_, err := io.Copy(right, left)
	if err != nil {
		logger.Errorf("%s\n%s\n", label, err.Error())
	}
}
