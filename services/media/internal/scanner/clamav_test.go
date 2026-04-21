package scanner

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func startMockClamd(t *testing.T, response string, delay time.Duration) (addr string, closeFn func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 1024)
				n, err := c.Read(buf)
				if err != nil {
					return
				}
				cmd := string(buf[:n])
				if !strings.Contains(cmd, "INSTREAM") {
					return
				}
				for {
					var chunkLen uint32
					if err := binary.Read(c, binary.BigEndian, &chunkLen); err != nil {
						return
					}
					if chunkLen == 0 {
						break
					}
					chunk := make([]byte, chunkLen)
					if _, err := io.ReadFull(c, chunk); err != nil {
						return
					}
				}
				if delay > 0 {
					time.Sleep(delay)
				}
				c.Write([]byte(response))
			}(conn)
		}
	}()
	return ln.Addr().String(), func() { ln.Close() }
}

func TestScan_Clean(t *testing.T) {
	addr, closeFn := startMockClamd(t, "stream: OK\x00", 0)
	defer closeFn()

	s := NewClamAVScanner(addr, 5*time.Second)
	ctx := context.Background()
	result, err := s.Scan(ctx, strings.NewReader("hello world"), "test.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Clean {
		t.Errorf("expected clean, got virus: %s", result.Virus)
	}
}

func TestScan_VirusDetected(t *testing.T) {
	addr, closeFn := startMockClamd(t, "stream: Eicar-Signature FOUND\x00", 0)
	defer closeFn()

	s := NewClamAVScanner(addr, 5*time.Second)
	ctx := context.Background()
	eicar := `X5O!P%%@AP[4\PZX54(P^)7CC)7}$EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*`
	result, err := s.Scan(ctx, strings.NewReader(eicar), "eicar.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Clean {
		t.Error("expected virus detected, got clean")
	}
	if result.Virus != "Eicar-Signature" {
		t.Errorf("expected virus 'Eicar-Signature', got: %s", result.Virus)
	}
}

func TestScan_Timeout(t *testing.T) {
	addr, closeFn := startMockClamd(t, "stream: OK\x00", 3*time.Second)
	defer closeFn()

	s := NewClamAVScanner(addr, 500*time.Millisecond)
	ctx := context.Background()
	_, err := s.Scan(ctx, strings.NewReader("hello"), "test.txt")
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

func TestScan_ConnectionRefused(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to get free port: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	s := NewClamAVScanner(addr, 2*time.Second)
	ctx := context.Background()
	_, err = s.Scan(ctx, strings.NewReader("hello"), "test.txt")
	if err == nil {
		t.Fatal("expected connection refused error, got nil")
	}
}

func TestNewClamAVScanner_EmptyAddr(t *testing.T) {
	s := NewClamAVScanner("", 5*time.Second)
	if s != nil {
		t.Error("expected nil scanner for empty addr")
	}
}
