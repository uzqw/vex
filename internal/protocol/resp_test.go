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
	"bytes"
	"math"
	"strings"
	"testing"
)

func TestFastVectorParser(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []float32
		wantErr  bool
	}{
		{
			name:     "simple vector",
			input:    "[0.1, 0.2, 0.3]",
			expected: []float32{0.1, 0.2, 0.3},
			wantErr:  false,
		},
		{
			name:     "no spaces",
			input:    "[0.1,0.2,0.3]",
			expected: []float32{0.1, 0.2, 0.3},
			wantErr:  false,
		},
		{
			name:     "with extra whitespace",
			input:    "  [  0.1 ,  0.2 ,  0.3  ]  ",
			expected: []float32{0.1, 0.2, 0.3},
			wantErr:  false,
		},
		{
			name:     "negative values",
			input:    "[-0.5, 0.5, -1.0]",
			expected: []float32{-0.5, 0.5, -1.0},
			wantErr:  false,
		},
		{
			name:     "empty vector",
			input:    "[]",
			expected: []float32{},
			wantErr:  false,
		},
		{
			name:     "single element",
			input:    "[0.5]",
			expected: []float32{0.5},
			wantErr:  false,
		},
		{
			name:    "missing brackets",
			input:   "0.1, 0.2, 0.3",
			wantErr: true,
		},
		{
			name:    "missing opening bracket",
			input:   "0.1, 0.2]",
			wantErr: true,
		},
		{
			name:    "invalid number",
			input:   "[0.1, abc, 0.3]",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FastVectorParser(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("FastVectorParser() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if len(got) != len(tt.expected) {
					t.Errorf("FastVectorParser() returned %d elements, want %d", len(got), len(tt.expected))
					return
				}
				for i := range got {
					if math.Abs(float64(got[i]-tt.expected[i])) > 0.0001 {
						t.Errorf("FastVectorParser()[%d] = %v, want %v", i, got[i], tt.expected[i])
					}
				}
			}
		})
	}
}

func TestRESPReader(t *testing.T) {
	t.Run("read array command", func(t *testing.T) {
		input := "*3\r\n$4\r\nVSET\r\n$5\r\nmykey\r\n$10\r\n[0.1, 0.2]\r\n"
		reader := NewRESPReader(strings.NewReader(input))

		cmd, err := reader.ReadCommand()
		if err != nil {
			t.Fatalf("ReadCommand() error = %v", err)
		}

		expected := []string{"VSET", "mykey", "[0.1, 0.2]"}
		if len(cmd) != len(expected) {
			t.Fatalf("ReadCommand() returned %d elements, want %d", len(cmd), len(expected))
		}

		for i := range cmd {
			if cmd[i] != expected[i] {
				t.Errorf("cmd[%d] = %q, want %q", i, cmd[i], expected[i])
			}
		}
	})

	t.Run("read simple string", func(t *testing.T) {
		input := "+OK\r\n"
		reader := NewRESPReader(strings.NewReader(input))

		cmd, err := reader.ReadCommand()
		if err != nil {
			t.Fatalf("ReadCommand() error = %v", err)
		}

		if len(cmd) != 1 || cmd[0] != "OK" {
			t.Errorf("ReadCommand() = %v, want [OK]", cmd)
		}
	})

	t.Run("read PING command", func(t *testing.T) {
		input := "*1\r\n$4\r\nPING\r\n"
		reader := NewRESPReader(strings.NewReader(input))

		cmd, err := reader.ReadCommand()
		if err != nil {
			t.Fatalf("ReadCommand() error = %v", err)
		}

		if len(cmd) != 1 || cmd[0] != "PING" {
			t.Errorf("ReadCommand() = %v, want [PING]", cmd)
		}
	})

	t.Run("read integer", func(t *testing.T) {
		input := ":1000\r\n"
		reader := NewRESPReader(strings.NewReader(input))

		cmd, err := reader.ReadCommand()
		if err != nil {
			t.Fatalf("ReadCommand() error = %v", err)
		}

		if len(cmd) != 1 || cmd[0] != "1000" {
			t.Errorf("ReadCommand() = %v, want [1000]", cmd)
		}
	})

	t.Run("read bulk string directly", func(t *testing.T) {
		input := "$5\r\nhello\r\n"
		reader := NewRESPReader(strings.NewReader(input))

		cmd, err := reader.ReadCommand()
		if err != nil {
			t.Fatalf("ReadCommand() error = %v", err)
		}

		if len(cmd) != 1 || cmd[0] != "hello" {
			t.Errorf("ReadCommand() = %v, want [hello]", cmd)
		}
	})

	t.Run("read null bulk string", func(t *testing.T) {
		input := "$-1\r\n"
		reader := NewRESPReader(strings.NewReader(input))

		cmd, err := reader.ReadCommand()
		if err != nil {
			t.Fatalf("ReadCommand() error = %v", err)
		}

		if len(cmd) != 1 || cmd[0] != "" {
			t.Errorf("ReadCommand() = %v, want ['']", cmd)
		}
	})

	t.Run("invalid type byte", func(t *testing.T) {
		input := "XPING\r\n"
		reader := NewRESPReader(strings.NewReader(input))

		_, err := reader.ReadCommand()
		if err == nil {
			t.Error("ReadCommand() expected error for invalid type byte")
		}
	})

	t.Run("invalid array length", func(t *testing.T) {
		input := "*abc\r\n"
		reader := NewRESPReader(strings.NewReader(input))

		_, err := reader.ReadCommand()
		if err == nil {
			t.Error("ReadCommand() expected error for invalid array length")
		}
	})

	t.Run("invalid bulk string length", func(t *testing.T) {
		input := "$abc\r\n"
		reader := NewRESPReader(strings.NewReader(input))

		_, err := reader.ReadCommand()
		if err == nil {
			t.Error("ReadCommand() expected error for invalid bulk string length")
		}
	})

	t.Run("negative bulk string length", func(t *testing.T) {
		input := "$-5\r\n"
		reader := NewRESPReader(strings.NewReader(input))

		_, err := reader.ReadCommand()
		if err == nil {
			t.Error("ReadCommand() expected error for negative bulk string length")
		}
	})

	t.Run("read error response", func(t *testing.T) {
		input := "-ERR unknown command\r\n"
		reader := NewRESPReader(strings.NewReader(input))

		_, err := reader.ReadCommand()
		if err == nil {
			t.Error("ReadCommand() expected error for error response")
		}
	})
}

func TestRESPWriter(t *testing.T) {
	t.Run("write simple string", func(t *testing.T) {
		var buf bytes.Buffer
		writer := NewRESPWriter(&buf)

		err := writer.WriteSimpleString("OK")
		if err != nil {
			t.Fatalf("WriteSimpleString() error = %v", err)
		}
		writer.Flush()

		expected := "+OK\r\n"
		if buf.String() != expected {
			t.Errorf("WriteSimpleString() output = %q, want %q", buf.String(), expected)
		}
	})

	t.Run("write error", func(t *testing.T) {
		var buf bytes.Buffer
		writer := NewRESPWriter(&buf)

		err := writer.WriteError("something went wrong")
		if err != nil {
			t.Fatalf("WriteError() error = %v", err)
		}
		writer.Flush()

		expected := "-ERR something went wrong\r\n"
		if buf.String() != expected {
			t.Errorf("WriteError() output = %q, want %q", buf.String(), expected)
		}
	})

	t.Run("write bulk string", func(t *testing.T) {
		var buf bytes.Buffer
		writer := NewRESPWriter(&buf)

		err := writer.WriteBulkString("hello")
		if err != nil {
			t.Fatalf("WriteBulkString() error = %v", err)
		}
		writer.Flush()

		expected := "$5\r\nhello\r\n"
		if buf.String() != expected {
			t.Errorf("WriteBulkString() output = %q, want %q", buf.String(), expected)
		}
	})

	t.Run("write integer", func(t *testing.T) {
		var buf bytes.Buffer
		writer := NewRESPWriter(&buf)

		err := writer.WriteInteger(42)
		if err != nil {
			t.Fatalf("WriteInteger() error = %v", err)
		}
		writer.Flush()

		expected := ":42\r\n"
		if buf.String() != expected {
			t.Errorf("WriteInteger() output = %q, want %q", buf.String(), expected)
		}
	})

	t.Run("write array", func(t *testing.T) {
		var buf bytes.Buffer
		writer := NewRESPWriter(&buf)

		err := writer.WriteArray([]string{"key1", "key2"})
		if err != nil {
			t.Fatalf("WriteArray() error = %v", err)
		}
		writer.Flush()

		expected := "*2\r\n$4\r\nkey1\r\n$4\r\nkey2\r\n"
		if buf.String() != expected {
			t.Errorf("WriteArray() output = %q, want %q", buf.String(), expected)
		}
	})
}

func BenchmarkFastVectorParser(b *testing.B) {
	input := "[0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0]"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = FastVectorParser(input)
	}
}

func BenchmarkRESPReader(b *testing.B) {
	input := "*3\r\n$4\r\nVSET\r\n$5\r\nmykey\r\n$50\r\n[0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0]\r\n"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader := NewRESPReader(strings.NewReader(input))
		_, _ = reader.ReadCommand()
	}
}
