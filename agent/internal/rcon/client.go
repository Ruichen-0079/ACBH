package rcon

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

const (
	packetTypeResponse = 0
	packetTypeCommand  = 2
	packetTypeAuth     = 3
	maxPacketBytes     = 4 * 1024 * 1024
)

type Config struct {
	Host     string
	Port     int
	Password string
	Timeout  time.Duration
}

type Client struct {
	conn      net.Conn
	timeout   time.Duration
	requestID int32
}

func Dial(ctx context.Context, config Config) (*Client, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}

	address := net.JoinHostPort(config.Host, strconv.Itoa(config.Port))
	dialer := net.Dialer{Timeout: config.Timeout}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("connect to RCON %s: %w", address, err)
	}

	client := &Client{
		conn:      conn,
		timeout:   config.Timeout,
		requestID: 1,
	}
	if err := client.authenticate(config.Password); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return client, nil
}

func Execute(ctx context.Context, config Config, command string) (string, error) {
	client, err := Dial(ctx, config)
	if err != nil {
		return "", err
	}
	defer client.Close()
	return client.Command(command)
}

func (client *Client) Command(command string) (string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "", errors.New("RCON command is required")
	}

	requestID := client.nextRequestID()
	if err := client.writePacket(packet{
		RequestID: requestID,
		Type:      packetTypeCommand,
		Payload:   command,
	}); err != nil {
		return "", fmt.Errorf("send RCON command: %w", err)
	}

	response, err := client.readPacket()
	if err != nil {
		return "", fmt.Errorf("read RCON command response: %w", err)
	}
	if response.RequestID != requestID {
		return "", fmt.Errorf("RCON command response ID %d does not match request ID %d", response.RequestID, requestID)
	}
	if response.Type != packetTypeResponse {
		return "", fmt.Errorf("unexpected RCON command response type %d", response.Type)
	}
	return response.Payload, nil
}

func (client *Client) Close() error {
	if client == nil || client.conn == nil {
		return nil
	}
	return client.conn.Close()
}

func (client *Client) authenticate(password string) error {
	requestID := client.nextRequestID()
	if err := client.writePacket(packet{
		RequestID: requestID,
		Type:      packetTypeAuth,
		Payload:   password,
	}); err != nil {
		return fmt.Errorf("send RCON authentication: %w", err)
	}

	response, err := client.readPacket()
	if err != nil {
		return fmt.Errorf("read RCON authentication response: %w", err)
	}
	if response.RequestID == -1 {
		return errors.New("RCON authentication failed")
	}
	if response.RequestID != requestID {
		return fmt.Errorf("RCON authentication response ID %d does not match request ID %d", response.RequestID, requestID)
	}
	if response.Type != packetTypeCommand {
		return fmt.Errorf("unexpected RCON authentication response type %d", response.Type)
	}
	return nil
}

func (client *Client) nextRequestID() int32 {
	requestID := client.requestID
	client.requestID++
	return requestID
}

func (client *Client) writePacket(value packet) error {
	if err := client.conn.SetWriteDeadline(time.Now().Add(client.timeout)); err != nil {
		return err
	}
	data, err := encodePacket(value)
	if err != nil {
		return err
	}
	_, err = io.Copy(client.conn, bytes.NewReader(data))
	return err
}

func (client *Client) readPacket() (packet, error) {
	if err := client.conn.SetReadDeadline(time.Now().Add(client.timeout)); err != nil {
		return packet{}, err
	}
	return decodePacket(client.conn)
}

func validateConfig(config Config) error {
	switch {
	case strings.TrimSpace(config.Host) == "":
		return errors.New("RCON host is required")
	case config.Port < 1 || config.Port > 65535:
		return fmt.Errorf("RCON port %d is outside 1-65535", config.Port)
	case config.Password == "":
		return errors.New("RCON password is required")
	case config.Timeout <= 0:
		return errors.New("RCON timeout must be positive")
	default:
		return nil
	}
}

type packet struct {
	RequestID int32
	Type      int32
	Payload   string
}

func encodePacket(value packet) ([]byte, error) {
	payload := []byte(value.Payload)
	length := 4 + 4 + len(payload) + 2
	if length > maxPacketBytes {
		return nil, fmt.Errorf("RCON packet exceeds %d bytes", maxPacketBytes)
	}

	data := make([]byte, 4+length)
	binary.LittleEndian.PutUint32(data[0:4], uint32(length))
	binary.LittleEndian.PutUint32(data[4:8], uint32(value.RequestID))
	binary.LittleEndian.PutUint32(data[8:12], uint32(value.Type))
	copy(data[12:], payload)
	return data, nil
}

func decodePacket(reader io.Reader) (packet, error) {
	var sizeBytes [4]byte
	if _, err := io.ReadFull(reader, sizeBytes[:]); err != nil {
		return packet{}, err
	}
	size := int(binary.LittleEndian.Uint32(sizeBytes[:]))
	if size < 10 || size > maxPacketBytes {
		return packet{}, fmt.Errorf("invalid RCON packet size %d", size)
	}

	data := make([]byte, size)
	if _, err := io.ReadFull(reader, data); err != nil {
		return packet{}, err
	}
	if data[size-2] != 0 || data[size-1] != 0 {
		return packet{}, errors.New("RCON packet is missing terminators")
	}

	return packet{
		RequestID: int32(binary.LittleEndian.Uint32(data[0:4])),
		Type:      int32(binary.LittleEndian.Uint32(data[4:8])),
		Payload:   string(data[8 : size-2]),
	}, nil
}
