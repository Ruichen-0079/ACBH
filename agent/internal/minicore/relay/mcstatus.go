package relay

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"time"
)

const defaultMCPingTimeout = 4 * time.Second

func pingMinecraftStatus(ctx context.Context, address string, timeout time.Duration) (bool, error) {
	if timeout <= 0 {
		timeout = defaultMCPingTimeout
	}
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))

	host, portStr, splitErr := net.SplitHostPort(address)
	if splitErr != nil {
		return false, splitErr
	}
	var port uint16
	if _, scanErr := fmt.Sscanf(portStr, "%d", &port); scanErr != nil {
		return false, scanErr
	}

	handshake := buildHandshakePacket(host, port, 1)
	if _, err := conn.Write(handshake); err != nil {
		return false, err
	}
	if _, err := conn.Write(buildPacket([]byte{0x00})); err != nil {
		return false, err
	}

	packet, err := readPacket(conn)
	if err != nil {
		return false, err
	}
	if len(packet) < 2 || packet[0] != 0x00 {
		return false, fmt.Errorf("unexpected status response packet id")
	}
	jsonPayload, err := readMCString(packet[1:])
	if err != nil {
		return false, err
	}
	var status map[string]any
	if err := json.Unmarshal([]byte(jsonPayload), &status); err != nil {
		return false, err
	}
	if _, ok := status["version"]; !ok {
		return false, fmt.Errorf("status json missing version field")
	}
	return true, nil
}

func buildHandshakePacket(host string, port uint16, nextState byte) []byte {
	var payload bytes.Buffer
	payload.WriteByte(0x00)
	writeVarInt(&payload, 47)
	writeMCString(&payload, host)
	_ = binary.Write(&payload, binary.BigEndian, port)
	writeVarInt(&payload, int32(nextState))
	return buildPacket(payload.Bytes())
}

func buildPacket(payload []byte) []byte {
	var packet bytes.Buffer
	writeVarInt(&packet, int32(len(payload)))
	packet.Write(payload)
	return packet.Bytes()
}

func writeVarInt(buf *bytes.Buffer, value int32) {
	for {
		b := byte(value & 0x7F)
		value >>= 7
		if value != 0 {
			b |= 0x80
		}
		buf.WriteByte(b)
		if value == 0 {
			break
		}
	}
}

func writeMCString(buf *bytes.Buffer, value string) {
	writeVarInt(buf, int32(len(value)))
	buf.WriteString(value)
}

func readPacket(conn net.Conn) ([]byte, error) {
	length, err := readVarInt(conn)
	if err != nil {
		return nil, err
	}
	if length <= 0 || length > 1<<20 {
		return nil, fmt.Errorf("invalid packet length %d", length)
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func readVarInt(conn net.Conn) (int, error) {
	var numRead int
	var result int
	for {
		if numRead > 5 {
			return 0, fmt.Errorf("varint too long")
		}
		var b [1]byte
		if _, err := io.ReadFull(conn, b[:]); err != nil {
			return 0, err
		}
		value := int(b[0] & 0x7F)
		result |= value << (7 * numRead)
		numRead++
		if (b[0] & 0x80) == 0 {
			break
		}
	}
	return result, nil
}

func readMCString(payload []byte) (string, error) {
	reader := bytes.NewReader(payload)
	length, err := readVarIntFromReader(reader)
	if err != nil {
		return "", err
	}
	if length < 0 || length > len(payload) {
		return "", fmt.Errorf("invalid string length %d", length)
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(reader, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}

func readVarIntFromReader(reader *bytes.Reader) (int, error) {
	var numRead int
	var result int
	for {
		if numRead > 5 {
			return 0, fmt.Errorf("varint too long")
		}
		b, err := reader.ReadByte()
		if err != nil {
			return 0, err
		}
		value := int(b & 0x7F)
		result |= value << (7 * numRead)
		numRead++
		if (b & 0x80) == 0 {
			break
		}
	}
	return result, nil
}