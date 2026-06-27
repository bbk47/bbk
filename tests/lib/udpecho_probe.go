//go:build ignore

// udpecho_probe —— 经 SOCKS5 UDP ASSOCIATE 的端到端 UDP 中继压测：
// 内置本地 UDP echo server(可多个)，通过隧道把各种大小/并发的数据报打到目标并校验回显。
// 覆盖场景：小包 / 大包(近 64KB) / 高并发 / 多目标(NAT 会话表) / 分片(FRAG!=0)丢弃。
//
// 用法: go run tests/lib/udpecho_probe.go -socks 127.0.0.1:1090 -conc 100 -targets 3
// 退出码 0=全部用例通过, 1=任一失败。
package main

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	socks := flag.String("socks", "127.0.0.1:1090", "socks5 address")
	conc := flag.Int("conc", 100, "并发数据报数量(单关联内)")
	targets := flag.Int("targets", 3, "并行 UDP 目标(echo server)数量, 验证 NAT 会话表")
	flag.Parse()

	var fails int

	// 用例1a: 多种大小(含近 64KB 大包)顺序收发, 严格逐字节校验(确定性, 必须零丢失)
	if err := caseSizesSequential(*socks); err != nil {
		fmt.Fprintln(os.Stderr, "CASE sizes-sequential FAIL:", err)
		fails++
	} else {
		fmt.Println("OK  sizes-sequential (含 60KB 大包, 逐字节一致)")
	}

	// 用例1b: 高并发 —— 用一池独立 UDP 关联(各自一发一收+丢包重传, 模拟真实 UDP 业务)
	// 并发跑 conc 个数据报, 逐字节校验。bounded in-flight 避免突发溢出, 重传兜住偶发丢包。
	if err := caseConcurrent(*socks, *conc); err != nil {
		fmt.Fprintln(os.Stderr, "CASE concurrent FAIL:", err)
		fails++
	} else {
		fmt.Printf("OK  concurrent (%d 数据报跨多关联并发, 逐字节一致)\n", *conc)
	}

	// 用例2: 多目标(NAT 会话表) —— 一条关联流并发打到多个 echo server
	if err := caseMultiTarget(*socks, *targets); err != nil {
		fmt.Fprintln(os.Stderr, "CASE multi-target FAIL:", err)
		fails++
	} else {
		fmt.Println("OK  multi-target")
	}

	// 用例3: 分片(FRAG!=0)必须被丢弃，且不影响后续正常数据报
	if err := caseFragDrop(*socks); err != nil {
		fmt.Fprintln(os.Stderr, "CASE frag-drop FAIL:", err)
		fails++
	} else {
		fmt.Println("OK  frag-drop")
	}

	if fails > 0 {
		fmt.Fprintf(os.Stderr, "udpecho_probe FAIL: %d case(s) failed\n", fails)
		os.Exit(1)
	}
	fmt.Println("udpecho_probe OK (all cases passed)")
}

// ---------- 用例实现 ----------

// caseSizesSequential 顺序发送多种大小(含近 64KB 大包)的数据报并严格逐字节校验。
// 一发一收、互不重叠, 不触发突发丢包, 因此必须零丢失——用于确定性验证大包与完整性。
func caseSizesSequential(socks string) error {
	echo, echoAddr, err := newUDPEcho()
	if err != nil {
		return err
	}
	defer echo.Close()

	uc, _, err := udpAssociate(socks)
	if err != nil {
		return err
	}
	defer uc.Close()

	sizes := []int{1, 64, 512, 1400, 8192, 32768, 60000} // 含近 64KB 大包
	buf := make([]byte, 70000)
	for _, sz := range sizes {
		body := make([]byte, sz)
		_, _ = rand.Read(body)
		dg := buildUDPDatagram(0x00, echoAddr, body)
		_ = uc.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if _, err := uc.Write(dg); err != nil {
			return fmt.Errorf("write size=%d: %w", sz, err)
		}
		_ = uc.SetReadDeadline(time.Now().Add(5 * time.Second))
		n, err := uc.Read(buf)
		if err != nil {
			return fmt.Errorf("read size=%d: %w", sz, err)
		}
		hdr, err := socks5UDPHeaderLen(buf[:n])
		if err != nil {
			return err
		}
		if got := buf[hdr:n]; !bytes.Equal(got, body) {
			return fmt.Errorf("size=%d echo mismatch (got %d bytes)", sz, len(got))
		}
	}
	return nil
}

// caseConcurrent 用一池独立 UDP 关联并发跑 conc 个数据报回显, 逐字节校验。
// 每个 worker 持有自己的关联, 一发一收(bounded in-flight=1), 偶发丢包按 UDP 业务惯例重传,
// 因此 localhost 下应达成 100% 投递。既验证并发(多关联/多流), 又确定性地保证完整性。
func caseConcurrent(socks string, conc int) error {
	echo, echoAddr, err := newUDPEcho()
	if err != nil {
		return err
	}
	defer echo.Close()

	workers := conc
	if workers > 32 {
		workers = 32
	}
	sizes := []int{64, 256, 512, 1024, 1400} // 模拟真实 UDP 业务(QUIC/DNS 量级)

	var (
		wg      sync.WaitGroup
		nextID  uint64
		failCnt int64
		errOnce sync.Once
		firstE  error
	)
	for w := 0; w < workers; w++ {
		// 该 worker 负责的数据报数量(把 conc 尽量均分)。
		share := conc / workers
		if w < conc%workers {
			share++
		}
		if share == 0 {
			continue
		}
		wg.Add(1)
		go func(w, share int) {
			defer wg.Done()
			uc, _, err := udpAssociate(socks)
			if err != nil {
				errOnce.Do(func() { firstE = fmt.Errorf("worker %d associate: %w", w, err) })
				atomic.AddInt64(&failCnt, int64(share))
				return
			}
			defer uc.Close()
			buf := make([]byte, 4096)
			for k := 0; k < share; k++ {
				id := atomic.AddUint64(&nextID, 1)
				sz := sizes[int(id)%len(sizes)]
				body := make([]byte, sz)
				_, _ = rand.Read(body)
				binary.BigEndian.PutUint64(body, id)
				dg := buildUDPDatagram(0x00, echoAddr, body)
				ok := false
				for try := 0; try < 5 && !ok; try++ { // 丢包重传(UDP 业务惯例)
					_ = uc.SetWriteDeadline(time.Now().Add(2 * time.Second))
					if _, err := uc.Write(dg); err != nil {
						continue
					}
					_ = uc.SetReadDeadline(time.Now().Add(400 * time.Millisecond))
					n, err := uc.Read(buf)
					if err != nil {
						continue // 超时->重传
					}
					hdr, err := socks5UDPHeaderLen(buf[:n])
					if err != nil {
						continue
					}
					payload := buf[hdr:n]
					if len(payload) < 8 || binary.BigEndian.Uint64(payload) != id {
						continue // 串到别的回包(本 worker 独占关联, 正常不会发生)->重读
					}
					if !bytes.Equal(payload, body) {
						errOnce.Do(func() { firstE = fmt.Errorf("echo body mismatch id=%d", id) })
						atomic.AddInt64(&failCnt, 1)
						ok = true // 视为已处理(失败)
						break
					}
					ok = true
				}
				if !ok {
					atomic.AddInt64(&failCnt, 1)
				}
			}
		}(w, share)
	}
	wg.Wait()
	if firstE != nil {
		return firstE
	}
	if failCnt > 0 {
		return fmt.Errorf("%d/%d 数据报重传 5 次仍失败", failCnt, conc)
	}
	return nil
}

// caseMultiTarget 验证一条关联流可同时发往多个目标(server 侧 NAT 会话表)。
func caseMultiTarget(socks string, targets int) error {
	if targets < 1 {
		targets = 1
	}
	echos := make([]*net.UDPConn, 0, targets)
	addrs := make([]*net.UDPAddr, 0, targets)
	defer func() {
		for _, e := range echos {
			_ = e.Close()
		}
	}()
	for i := 0; i < targets; i++ {
		e, a, err := newUDPEcho()
		if err != nil {
			return err
		}
		echos = append(echos, e)
		addrs = append(addrs, a)
	}

	uc, _, err := udpAssociate(socks)
	if err != nil {
		return err
	}
	defer uc.Close()

	// 每个目标发一条带目标序号的数据报
	for i, a := range addrs {
		body := []byte(fmt.Sprintf("target-%d-hello", i))
		dg := buildUDPDatagram(0x00, a, body)
		_ = uc.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if _, err := uc.Write(dg); err != nil {
			return fmt.Errorf("write target %d: %w", i, err)
		}
	}

	// 收齐 targets 条回显（按回包内目标地址区分）
	seen := make(map[string]bool)
	buf := make([]byte, 4096)
	deadline := time.Now().Add(8 * time.Second)
	for len(seen) < targets {
		_ = uc.SetReadDeadline(deadline)
		n, err := uc.Read(buf)
		if err != nil {
			return fmt.Errorf("only %d/%d targets echoed: %w", len(seen), targets, err)
		}
		hdr, err := socks5UDPHeaderLen(buf[:n])
		if err != nil {
			return err
		}
		seen[string(buf[hdr:n])] = true
	}
	return nil
}

// caseFragDrop 验证 FRAG!=0 的分片数据报被丢弃，且不影响紧随其后的正常数据报。
func caseFragDrop(socks string) error {
	echo, echoAddr, err := newUDPEcho()
	if err != nil {
		return err
	}
	defer echo.Close()

	uc, _, err := udpAssociate(socks)
	if err != nil {
		return err
	}
	defer uc.Close()

	// 1) 分片包(FRAG=1)：应被 client 侧丢弃，隧道里不会有任何字节
	frag := buildUDPDatagram(0x01, echoAddr, []byte("must-be-dropped"))
	_ = uc.SetWriteDeadline(time.Now().Add(3 * time.Second))
	if _, err := uc.Write(frag); err != nil {
		return fmt.Errorf("write frag: %w", err)
	}

	// 2) 正常包：必须正常回显
	want := []byte("normal-after-frag")
	dg := buildUDPDatagram(0x00, echoAddr, want)
	if _, err := uc.Write(dg); err != nil {
		return fmt.Errorf("write normal: %w", err)
	}

	buf := make([]byte, 4096)
	_ = uc.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, err := uc.Read(buf)
	if err != nil {
		return fmt.Errorf("read echo: %w", err)
	}
	hdr, err := socks5UDPHeaderLen(buf[:n])
	if err != nil {
		return err
	}
	got := buf[hdr:n]
	if bytes.Equal(got, []byte("must-be-dropped")) {
		return fmt.Errorf("fragmented datagram was NOT dropped")
	}
	if !bytes.Equal(got, want) {
		return fmt.Errorf("echo mismatch: got %q want %q", got, want)
	}
	return nil
}

// ---------- 公共工具 ----------

func newUDPEcho() (*net.UDPConn, *net.UDPAddr, error) {
	c, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		return nil, nil, fmt.Errorf("listen echo: %w", err)
	}
	go func() {
		buf := make([]byte, 70000)
		for {
			n, src, err := c.ReadFromUDP(buf)
			if err != nil {
				return
			}
			_, _ = c.WriteToUDP(buf[:n], src)
		}
	}()
	return c, c.LocalAddr().(*net.UDPAddr), nil
}

// udpAssociate 完成 SOCKS5 握手 + UDP ASSOCIATE，返回连到 relay 的 UDP socket。
// 注意：必须保持返回的控制连接不关闭（关闭即代表关联结束），这里通过让它逃逸到
// 一个后台 goroutine 读阻塞来保活，直到进程退出。
func udpAssociate(socks string) (*net.UDPConn, string, error) {
	ctrl, err := net.DialTimeout("tcp", socks, 5*time.Second)
	if err != nil {
		return nil, "", fmt.Errorf("dial socks: %w", err)
	}
	_ = ctrl.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := ctrl.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		ctrl.Close()
		return nil, "", fmt.Errorf("greeting: %w", err)
	}
	rep := make([]byte, 2)
	if _, err := io.ReadFull(ctrl, rep); err != nil || rep[1] != 0x00 {
		ctrl.Close()
		return nil, "", fmt.Errorf("method reply: %v err=%v", rep, err)
	}
	if _, err := ctrl.Write([]byte{0x05, 0x03, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
		ctrl.Close()
		return nil, "", fmt.Errorf("associate req: %w", err)
	}
	host, port, err := readSocks5Reply(ctrl)
	if err != nil {
		ctrl.Close()
		return nil, "", fmt.Errorf("associate reply: %w", err)
	}
	if host == "0.0.0.0" || host == "::" {
		host, _, _ = net.SplitHostPort(socks)
	}
	relay := net.JoinHostPort(host, fmt.Sprintf("%d", port))

	// 保活：清掉 deadline 并后台读，避免控制连接被 GC 关闭。
	_ = ctrl.SetDeadline(time.Time{})
	go func() { _, _ = io.Copy(io.Discard, ctrl) }()

	raddr, err := net.ResolveUDPAddr("udp", relay)
	if err != nil {
		ctrl.Close()
		return nil, "", fmt.Errorf("resolve relay: %w", err)
	}
	uc, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		ctrl.Close()
		return nil, "", fmt.Errorf("dial relay: %w", err)
	}
	return uc, relay, nil
}

// buildUDPDatagram 构造 SOCKS5 UDP 数据报: RSV(2) FRAG ATYP=IPv4 DST.ADDR DST.PORT DATA
func buildUDPDatagram(frag byte, dst *net.UDPAddr, payload []byte) []byte {
	var b bytes.Buffer
	b.Write([]byte{0x00, 0x00, frag})
	b.WriteByte(0x01)
	b.Write(dst.IP.To4())
	_ = binary.Write(&b, binary.BigEndian, uint16(dst.Port))
	b.Write(payload)
	return b.Bytes()
}

func readSocks5Reply(r io.Reader) (string, int, error) {
	head := make([]byte, 4)
	if _, err := io.ReadFull(r, head); err != nil {
		return "", 0, err
	}
	if head[0] != 0x05 || head[1] != 0x00 {
		return "", 0, fmt.Errorf("reply err ver=%d rep=%d", head[0], head[1])
	}
	var host string
	switch head[3] {
	case 0x01:
		b := make([]byte, 4)
		if _, err := io.ReadFull(r, b); err != nil {
			return "", 0, err
		}
		host = net.IP(b).String()
	case 0x03:
		l := make([]byte, 1)
		if _, err := io.ReadFull(r, l); err != nil {
			return "", 0, err
		}
		b := make([]byte, int(l[0]))
		if _, err := io.ReadFull(r, b); err != nil {
			return "", 0, err
		}
		host = string(b)
	case 0x04:
		b := make([]byte, 16)
		if _, err := io.ReadFull(r, b); err != nil {
			return "", 0, err
		}
		host = net.IP(b).String()
	default:
		return "", 0, fmt.Errorf("unknown atyp %d", head[3])
	}
	pb := make([]byte, 2)
	if _, err := io.ReadFull(r, pb); err != nil {
		return "", 0, err
	}
	return host, int(binary.BigEndian.Uint16(pb)), nil
}

func socks5UDPHeaderLen(b []byte) (int, error) {
	if len(b) < 4 {
		return 0, fmt.Errorf("udp resp too short")
	}
	switch b[3] {
	case 0x01:
		return 4 + 4 + 2, nil
	case 0x03:
		if len(b) < 5 {
			return 0, fmt.Errorf("udp resp domain short")
		}
		return 4 + 1 + int(b[4]) + 2, nil
	case 0x04:
		return 4 + 16 + 2, nil
	default:
		return 0, fmt.Errorf("udp resp bad atyp %d", b[3])
	}
}
