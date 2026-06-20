package utils

import (
	"io"
	"sync"

	"github.com/bbk47/toolbox"
)

func Forward(left, right io.ReadWriteCloser, label string, logger *toolbox.Logger) {
	defer left.Close()
	defer right.Close()
	_, err := io.Copy(right, left)
	if err != nil {
		logger.Debugf("%s\n%s\n", label, err.Error())
	}
}

type halfCloser interface {
	CloseWrite() error
}

// Relay 在 a、b 之间双向转发，支持半关闭：
// 某个方向读到 EOF 时，只关闭目标端的写端（对 Stream 即发 FIN、对 TCP 即 CloseWrite），
// 保留反方向继续传输；两个方向都结束后再整体关闭。
// 若某端不支持 CloseWrite，则退化为旧行为（直接整体关闭）。
func Relay(a, b io.ReadWriteCloser, logger *toolbox.Logger) {
	var once sync.Once
	closeBoth := func() {
		once.Do(func() {
			_ = a.Close()
			_ = b.Close()
		})
	}
	pipe := func(dst, src io.ReadWriteCloser, label string) {
		_, err := io.Copy(dst, src)
		if err != nil && logger != nil {
			logger.Debugf("%s\n%s\n", label, err.Error())
		}
		if hc, ok := dst.(halfCloser); ok {
			_ = hc.CloseWrite()
		} else {
			closeBoth()
		}
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); pipe(a, b, "b->a") }()
	pipe(b, a, "a->b")
	wg.Wait()
	closeBoth()
}
