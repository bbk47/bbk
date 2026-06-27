//go:build ignore

// streamintegrity —— 并发多流完整性测试：内置 TCP echo server，经 SOCKS5 并发开 N 路流，
// 每路传输确定性伪随机负载并逐字节校验回显，用于检测 bbk mux/共享 CFB 流在高并发下是否损坏。
//
// 用法: go run tests/lib/streamintegrity.go -socks 127.0.0.1:1090 -conc 50 -bytes 2097152
// 退出码 0=全部字节一致, 1=出现损坏/错误。
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	socks := flag.String("socks", "127.0.0.1:1090", "socks5 address")
	conc := flag.Int("conc", 50, "concurrent streams")
	nbytes := flag.Int64("bytes", 2<<20, "bytes per stream")
	flag.Parse()

	// 1) echo server
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintln(os.Stderr, "listen echo:", err)
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
	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	echoPort, _ := strconv.Atoi(portStr)
	fmt.Printf("echo server :%d  socks=%s conc=%d bytes/stream=%d\n", echoPort, *socks, *conc, *nbytes)

	var (
		wg       sync.WaitGroup
		failCnt  int64
		okCnt    int64
		firstErr atomic.Value
	)
	start := time.Now()
	for i := 0; i < *conc; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if err := oneStream(*socks, echoPort, int64(id+1), *nbytes); err != nil {
				atomic.AddInt64(&failCnt, 1)
				if firstErr.Load() == nil {
					firstErr.Store(fmt.Sprintf("stream#%d: %v", id, err))
				}
			} else {
				atomic.AddInt64(&okCnt, 1)
			}
		}(i)
	}
	wg.Wait()
	dur := time.Since(start)

	fmt.Printf("result: ok=%d fail=%d total=%d elapsed=%s throughput=%.1fMB/s\n",
		okCnt, failCnt, *conc, dur.Round(time.Millisecond),
		float64(okCnt*(*nbytes))/1e6/dur.Seconds())
	if failCnt > 0 {
		fmt.Fprintln(os.Stderr, "FIRST ERROR:", firstErr.Load())
		os.Exit(1)
	}
	fmt.Println("ALL STREAMS BYTE-EXACT OK")
}

// oneStream 经 socks5 连到 echo server，写入 seed 决定的伪随机字节流并校验回显。
func oneStream(socks string, echoPort int, seed, total int64) error {
	conn, err := net.DialTimeout("tcp", socks, 8*time.Second)
	if err != nil {
		return fmt.Errorf("dial socks: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(120 * time.Second))

	if err := socks5Connect(conn, echoPort); err != nil {
		return err
	}

	// writer：seed 决定的随机字节，随机分块写，写完半关闭。
	werrCh := make(chan error, 1)
	go func() {
		wr := rand.New(rand.NewSource(seed))
		buf := make([]byte, 64*1024)
		var sent int64
		for sent < total {
			n := int64(1 + wr.Intn(60000))
			if sent+n > total {
				n = total - sent
			}
			fillPRNG(buf[:n], seed, sent)
			if _, err := conn.Write(buf[:n]); err != nil {
				werrCh <- fmt.Errorf("write: %w", err)
				return
			}
			sent += n
		}
		if cw, ok := conn.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		}
		werrCh <- nil
	}()

	// reader：逐字节校验等于同一 PRNG 序列。
	exp := make([]byte, 64*1024)
	got := make([]byte, 64*1024)
	var rcvd int64
	for rcvd < total {
		toRead := int64(len(got))
		if total-rcvd < toRead {
			toRead = total - rcvd
		}
		n, err := io.ReadFull(conn, got[:toRead])
		if n > 0 {
			fillPRNG(exp[:n], seed, rcvd)
			for i := 0; i < n; i++ {
				if got[i] != exp[i] {
					return fmt.Errorf("byte mismatch at offset %d (got %#x want %#x)", rcvd+int64(i), got[i], exp[i])
				}
			}
			rcvd += int64(n)
		}
		if err != nil {
			return fmt.Errorf("read at %d/%d: %w", rcvd, total, err)
		}
	}
	if werr := <-werrCh; werr != nil {
		return werr
	}
	if rcvd != total {
		return fmt.Errorf("short echo: got %d want %d", rcvd, total)
	}
	return nil
}

// fillPRNG 用 (seed, offset) 确定性地填充 buf，writer/reader 两侧一致即可校验。
func fillPRNG(buf []byte, seed, offset int64) {
	for i := range buf {
		x := uint64(seed)*1099511628211 + uint64(offset+int64(i))*0x9E3779B97F4A7C15
		x ^= x >> 33
		x *= 0xff51afd7ed558ccd
		x ^= x >> 33
		buf[i] = byte(x)
	}
}

func socks5Connect(conn net.Conn, port int) error {
	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		return fmt.Errorf("greeting: %w", err)
	}
	rep := make([]byte, 2)
	if _, err := io.ReadFull(conn, rep); err != nil || rep[1] != 0x00 {
		return fmt.Errorf("method reply: %v err=%v", rep, err)
	}
	req := []byte{0x05, 0x01, 0x00, 0x01, 127, 0, 0, 1, 0, 0}
	binary.BigEndian.PutUint16(req[8:], uint16(port))
	if _, err := conn.Write(req); err != nil {
		return fmt.Errorf("connect req: %w", err)
	}
	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		return fmt.Errorf("connect reply head: %w", err)
	}
	if head[1] != 0x00 {
		return fmt.Errorf("connect rep code=%d", head[1])
	}
	var skip int
	switch head[3] {
	case 0x01:
		skip = 4 + 2
	case 0x04:
		skip = 16 + 2
	case 0x03:
		l := make([]byte, 1)
		if _, err := io.ReadFull(conn, l); err != nil {
			return err
		}
		skip = int(l[0]) + 2
	}
	if skip > 0 {
		if _, err := io.ReadFull(conn, make([]byte, skip)); err != nil {
			return fmt.Errorf("connect reply addr: %w", err)
		}
	}
	return nil
}
