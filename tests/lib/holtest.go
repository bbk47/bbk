//go:build ignore

// holtest —— 队头阻塞(HoL)测试：测量小请求 RTT 在「空闲」与「有大流量并发」两种情况下的差异。
// bbk 所有 stream 共享一个 writeWorker 串行发送，大响应可能把小请求堵在队尾。
//
// 用法: go run tests/lib/holtest.go -socks 127.0.0.1:1090 -load 4 -samples 30
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"sync/atomic"
	"time"
)

func main() {
	socks := flag.String("socks", "127.0.0.1:1090", "socks5 address")
	load := flag.Int("load", 4, "并发大流数量(背景负载)")
	samples := flag.Int("samples", 30, "小请求采样次数")
	flag.Parse()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintln(os.Stderr, "listen:", err)
		os.Exit(1)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) { defer c.Close(); _, _ = io.Copy(c, c) }(c)
		}
	}()
	_, ps, _ := net.SplitHostPort(ln.Addr().String())
	var port int
	fmt.Sscanf(ps, "%d", &port)

	fmt.Printf("echo :%d socks=%s load=%d samples=%d\n", port, *socks, *load, *samples)

	base := measure(*socks, port, *samples)
	fmt.Printf("空闲 RTT(ms): %s\n", base.String())

	var stop int32
	for i := 0; i < *load; i++ {
		go floodStream(*socks, port, &stop)
	}
	time.Sleep(300 * time.Millisecond) // 等背景流跑起来
	loaded := measure(*socks, port, *samples)
	atomic.StoreInt32(&stop, 1)
	fmt.Printf("负载 RTT(ms): %s\n", loaded.String())

	ratio := loaded.p50 / base.p50
	fmt.Printf("HoL 放大(p50 负载/空闲): %.1fx\n", ratio)
	if ratio > 5 {
		fmt.Println("WARN: 小请求在大流量下延迟显著放大，存在队头阻塞")
	} else {
		fmt.Println("OK: 队头阻塞不明显")
	}
}

type stat struct{ p50, p90, max float64 }

func (s stat) String() string { return fmt.Sprintf("p50=%.1f p90=%.1f max=%.1f", s.p50, s.p90, s.max) }

func measure(socks string, port, n int) stat {
	ds := make([]float64, 0, n)
	for i := 0; i < n; i++ {
		d, err := pingPong(socks, port)
		if err == nil {
			ds = append(ds, float64(d.Microseconds())/1000.0)
		}
		time.Sleep(20 * time.Millisecond)
	}
	sort.Float64s(ds)
	if len(ds) == 0 {
		return stat{}
	}
	pick := func(q float64) float64 { return ds[int(q*float64(len(ds)-1)+0.5)] }
	return stat{p50: pick(0.5), p90: pick(0.9), max: ds[len(ds)-1]}
}

// pingPong：新建 socks 流，写 64B 读 64B，返回往返耗时。
func pingPong(socks string, port int) (time.Duration, error) {
	c, err := net.DialTimeout("tcp", socks, 5*time.Second)
	if err != nil {
		return 0, err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(15 * time.Second))
	if err := connect(c, port); err != nil {
		return 0, err
	}
	msg := make([]byte, 64)
	buf := make([]byte, 64)
	t0 := time.Now()
	if _, err := c.Write(msg); err != nil {
		return 0, err
	}
	if _, err := io.ReadFull(c, buf); err != nil {
		return 0, err
	}
	return time.Since(t0), nil
}

func floodStream(socks string, port int, stop *int32) {
	c, err := net.DialTimeout("tcp", socks, 5*time.Second)
	if err != nil {
		return
	}
	defer c.Close()
	if err := connect(c, port); err != nil {
		return
	}
	go func() { _, _ = io.Copy(io.Discard, c) }()
	buf := make([]byte, 64*1024)
	for atomic.LoadInt32(stop) == 0 {
		if _, err := c.Write(buf); err != nil {
			return
		}
	}
}

func connect(c net.Conn, port int) error {
	if _, err := c.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		return err
	}
	rep := make([]byte, 2)
	if _, err := io.ReadFull(c, rep); err != nil {
		return err
	}
	req := []byte{0x05, 0x01, 0x00, 0x01, 127, 0, 0, 1, 0, 0}
	binary.BigEndian.PutUint16(req[8:], uint16(port))
	if _, err := c.Write(req); err != nil {
		return err
	}
	head := make([]byte, 10)
	if _, err := io.ReadFull(c, head); err != nil {
		return err
	}
	if head[1] != 0x00 {
		return fmt.Errorf("connect rep=%d", head[1])
	}
	return nil
}
