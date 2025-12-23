// Copyright 2025 uzqw
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package protocol

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

var (
	ErrInvalidProtocol = errors.New("invalid RESP protocol format")
	ErrInvalidLength   = errors.New("invalid length in RESP message")
	ErrUnexpectedEOF   = errors.New("unexpected EOF while reading RESP")
)

// RESPReader handles reading and parsing RESP protocol messages
// Uses buffered I/O to reduce syscalls and improve performance
type RESPReader struct {
	reader *bufio.Reader
}

// NewRESPReader creates a new RESP reader
func NewRESPReader(r io.Reader) *RESPReader {
	return &RESPReader{
		reader: bufio.NewReader(r),
	}
}

// ReadCommand reads and parses a RESP array command
// Returns the command and its arguments
// Example: *3\r\n$3\r\nSET\r\n$5\r\nmykey\r\n$7\r\nmyvalue\r\n
// Returns: ["SET", "mykey", "myvalue"]
func (r *RESPReader) ReadCommand() ([]string, error) {
	// Read the first byte to determine the type
	typ, err := r.reader.ReadByte()
	if err != nil {
		return nil, err
	}

	switch typ {
	case '*':
		// Array type - this is what we expect for commands
		return r.readArray()
	case '+', '-', ':', '$':
		// Simple string, error, integer, or bulk string
		// These can be valid in some contexts, but for commands we expect arrays
		if err := r.reader.UnreadByte(); err != nil {
			return nil, err
		}
		// Try to read as a single element
		val, err := r.readValue()
		if err != nil {
			return nil, err
		}
		return []string{val}, nil
	default:
		return nil, fmt.Errorf("%w: unexpected type byte '%c'", ErrInvalidProtocol, typ)
	}
}

// readArray reads a RESP array
func (r *RESPReader) readArray() ([]string, error) {
	// Read array length
	line, err := r.readLine()
	if err != nil {
		return nil, err
	}

	count, err := strconv.Atoi(line)
	if err != nil || count < 0 {
		return nil, fmt.Errorf("%w: invalid array length '%s'", ErrInvalidLength, line)
	}

	// Read array elements
	result := make([]string, count)
	for i := 0; i < count; i++ {
		val, err := r.readValue()
		if err != nil {
			return nil, err
		}
		result[i] = val
	}

	return result, nil
}

// readValue reads a single RESP value (bulk string, simple string, etc.)
func (r *RESPReader) readValue() (string, error) {
	typ, err := r.reader.ReadByte()
	if err != nil {
		return "", err
	}

	switch typ {
	case '$':
		// Bulk string
		return r.readBulkString()
	case '+':
		// Simple string
		return r.readLine()
	case '-':
		// Error
		line, err := r.readLine()
		if err != nil {
			return "", err
		}
		return "", errors.New(line)
	case ':':
		// Integer
		return r.readLine()
	default:
		return "", fmt.Errorf("%w: unexpected type byte '%c'", ErrInvalidProtocol, typ)
	}
}

// readBulkString reads a RESP bulk string
func (r *RESPReader) readBulkString() (string, error) {
	// Read length
	line, err := r.readLine()
	if err != nil {
		return "", err
	}

	length, err := strconv.Atoi(line)
	if err != nil {
		return "", fmt.Errorf("%w: invalid bulk string length '%s'", ErrInvalidLength, line)
	}

	if length == -1 {
		// Null bulk string
		return "", nil
	}

	if length < 0 {
		return "", fmt.Errorf("%w: negative bulk string length %d", ErrInvalidLength, length)
	}

	// Read the actual string content
	buf := make([]byte, length+2) // +2 for \r\n
	_, err = io.ReadFull(r.reader, buf)
	if err != nil {
		return "", err
	}

	// Verify CRLF terminator
	if buf[length] != '\r' || buf[length+1] != '\n' {
		return "", fmt.Errorf("%w: missing CRLF after bulk string", ErrInvalidProtocol)
	}

	return string(buf[:length]), nil
}

// readLine reads a line terminated by \r\n
func (r *RESPReader) readLine() (string, error) {
	line, err := r.reader.ReadString('\n')
	if err != nil {
		return "", err
	}

	// Remove \r\n
	if len(line) < 2 || line[len(line)-2] != '\r' {
		return "", fmt.Errorf("%w: line not terminated with CRLF", ErrInvalidProtocol)
	}

	return line[:len(line)-2], nil
}

// RESPWriter handles writing RESP protocol messages
// Buffers output to reduce syscalls
type RESPWriter struct {
	writer *bufio.Writer
}

// NewRESPWriter creates a new RESP writer
func NewRESPWriter(w io.Writer) *RESPWriter {
	return &RESPWriter{
		writer: bufio.NewWriter(w),
	}
}

// WriteSimpleString writes a RESP simple string (+OK\r\n)
func (w *RESPWriter) WriteSimpleString(s string) error {
	if _, err := w.writer.WriteString("+"); err != nil {
		return err
	}
	if _, err := w.writer.WriteString(s); err != nil {
		return err
	}
	if _, err := w.writer.WriteString("\r\n"); err != nil {
		return err
	}
	return nil
}

// WriteError writes a RESP error (-ERR message\r\n)
func (w *RESPWriter) WriteError(msg string) error {
	if _, err := w.writer.WriteString("-ERR "); err != nil {
		return err
	}
	if _, err := w.writer.WriteString(msg); err != nil {
		return err
	}
	if _, err := w.writer.WriteString("\r\n"); err != nil {
		return err
	}
	return nil
}

// WriteBulkString writes a RESP bulk string ($6\r\nfoobar\r\n)
func (w *RESPWriter) WriteBulkString(s string) error {
	length := len(s)
	if _, err := w.writer.WriteString("$"); err != nil {
		return err
	}
	if _, err := w.writer.WriteString(strconv.Itoa(length)); err != nil {
		return err
	}
	if _, err := w.writer.WriteString("\r\n"); err != nil {
		return err
	}
	if _, err := w.writer.WriteString(s); err != nil {
		return err
	}
	if _, err := w.writer.WriteString("\r\n"); err != nil {
		return err
	}
	return nil
}

// WriteArray writes a RESP array
func (w *RESPWriter) WriteArray(elements []string) error {
	if _, err := w.writer.WriteString("*"); err != nil {
		return err
	}
	if _, err := w.writer.WriteString(strconv.Itoa(len(elements))); err != nil {
		return err
	}
	if _, err := w.writer.WriteString("\r\n"); err != nil {
		return err
	}

	for _, elem := range elements {
		if err := w.WriteBulkString(elem); err != nil {
			return err
		}
	}

	return nil
}

// WriteInteger writes a RESP integer (:1000\r\n)
func (w *RESPWriter) WriteInteger(n int64) error {
	if _, err := w.writer.WriteString(":"); err != nil {
		return err
	}
	if _, err := w.writer.WriteString(strconv.FormatInt(n, 10)); err != nil {
		return err
	}
	if _, err := w.writer.WriteString("\r\n"); err != nil {
		return err
	}
	return nil
}

// Flush flushes the buffered data to the underlying writer
func (w *RESPWriter) Flush() error {
	return w.writer.Flush()
}

// FastVectorParser parses a vector string without JSON overhead
// Expects format: "[0.1, 0.2, 0.3]" or "[0.1,0.2,0.3]"
// This is a performance optimization mentioned in the design doc
func FastVectorParser(s string) ([]float32, error) {
	s = strings.TrimSpace(s)

	// Check for brackets
	if len(s) < 2 || s[0] != '[' || s[len(s)-1] != ']' {
		return nil, errors.New("vector must be enclosed in brackets")
	}

	// Remove brackets
	s = s[1 : len(s)-1]

	// Handle empty vector
	s = strings.TrimSpace(s)
	if s == "" {
		return []float32{}, nil
	}

	// Split by comma
	parts := strings.Split(s, ",")
	result := make([]float32, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		val, err := strconv.ParseFloat(part, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid vector element '%s': %w", part, err)
		}
		result = append(result, float32(val))
	}

	return result, nil
}
