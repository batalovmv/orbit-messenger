// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package scanner

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

// ScanResult holds the result of a virus scan.
type ScanResult struct {
	Clean bool
	Virus string
	Error error
}

// Scanner defines the interface for scanning content.
type Scanner interface {
	Scan(ctx context.Context, reader io.Reader, filename string) (*ScanResult, error)
}

// ClamAVScanner implements Scanner using ClamAV's TCP INSTREAM protocol.
type ClamAVScanner struct {
	addr    string
	timeout time.Duration
}

// NewClamAVScanner creates a new ClamAVScanner. Returns nil if addr is empty.
func NewClamAVScanner(addr string, timeout time.Duration) *ClamAVScanner {
	if addr == "" {
		return nil
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &ClamAVScanner{
		addr:    addr,
		timeout: timeout,
	}
}

// Scan sends data to ClamAV via INSTREAM protocol and returns the scan result.
func (c *ClamAVScanner) Scan(ctx context.Context, reader io.Reader, filename string) (*ScanResult, error) {
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(c.timeout)
	}

	dialer := &net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", c.addr)
	if err != nil {
		return nil, fmt.Errorf("clamav: dial %s: %w", c.addr, err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(deadline); err != nil {
		return nil, fmt.Errorf("clamav: set deadline: %w", err)
	}

	// Send zINSTREAM\0 command.
	if _, err := conn.Write([]byte("zINSTREAM\x00")); err != nil {
		return nil, fmt.Errorf("clamav: send command: %w", err)
	}

	// Stream data in chunks.
	const chunkSize = 4096
	buf := make([]byte, chunkSize)
	lenBuf := make([]byte, 4)

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		n, readErr := reader.Read(buf)
		if n > 0 {
			binary.BigEndian.PutUint32(lenBuf, uint32(n))
			if _, err := conn.Write(lenBuf); err != nil {
				return nil, fmt.Errorf("clamav: write chunk length: %w", err)
			}
			if _, err := conn.Write(buf[:n]); err != nil {
				return nil, fmt.Errorf("clamav: write chunk data: %w", err)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("clamav: read source: %w", readErr)
		}
	}

	// Send terminating zero-length chunk.
	if _, err := conn.Write([]byte{0x00, 0x00, 0x00, 0x00}); err != nil {
		return nil, fmt.Errorf("clamav: send terminator: %w", err)
	}

	// Read response.
	resp, err := io.ReadAll(conn)
	if err != nil {
		return nil, fmt.Errorf("clamav: read response: %w", err)
	}

	return parseResponse(string(resp)), nil
}

// parseResponse interprets the ClamAV response string.
func parseResponse(resp string) *ScanResult {
	resp = strings.TrimRight(resp, "\x00\n\r")

	switch {
	case strings.HasSuffix(resp, "OK"):
		return &ScanResult{Clean: true}

	case strings.Contains(resp, "FOUND"):
		virus := ""
		prefix := "stream: "
		suffix := " FOUND"
		if idx := strings.Index(resp, prefix); idx >= 0 {
			inner := resp[idx+len(prefix):]
			if strings.HasSuffix(inner, suffix) {
				virus = strings.TrimSuffix(inner, suffix)
			} else {
				virus = inner
			}
		}
		return &ScanResult{Clean: false, Virus: virus}

	case strings.Contains(resp, "ERROR"):
		return &ScanResult{Clean: false, Error: fmt.Errorf("clamav: scanner error: %s", resp)}

	default:
		return &ScanResult{Clean: false, Error: fmt.Errorf("clamav: unexpected response: %s", resp)}
	}
}
