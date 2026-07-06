package relay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
)

func startMockMinecraftStatusServer() (addr string, stop func(), err error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	done := make(chan struct{})
	var once sync.Once
	stopFn := func() {
		once.Do(func() {
			_ = ln.Close()
			<-done
		})
	}
	go func() {
		defer close(done)
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			go handleMockMinecraftStatusConn(conn)
		}
	}()
	return ln.Addr().String(), stopFn, nil
}

func handleMockMinecraftStatusConn(conn net.Conn) {
	defer conn.Close()
	for {
		packet, err := readPacket(conn)
		if err != nil {
			return
		}
		if len(packet) == 0 {
			continue
		}
		switch packet[0] {
		case 0x00:
			if len(packet) == 1 {
				statusJSON, _ := json.Marshal(map[string]any{
					"version": map[string]any{"name": "ACBH Mock", "protocol": 47},
					"players": map[string]any{"max": 20, "online": 0},
					"description": map[string]any{"text": "ACBH relay test"},
				})
				var payload bytes.Buffer
				payload.WriteByte(0x00)
				writeMCString(&payload, string(statusJSON))
				if _, err := conn.Write(buildPacket(payload.Bytes())); err != nil {
					return
				}
				continue
			}
		default:
			return
		}
	}
}

func readPacketFromConn(conn net.Conn) ([]byte, error) {
	return readPacket(conn)
}

func drainConn(conn net.Conn) error {
	buf := make([]byte, 4096)
	for {
		_, err := conn.Read(buf)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

func mustSplitHostPort(addr string) (string, int, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0, err
	}
	var port int
	if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil {
		return "", 0, err
	}
	return host, port, nil
}