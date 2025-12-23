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
	"errors"
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
		{"simple", "[0.1, 0.2, 0.3]", []float32{0.1, 0.2, 0.3}, false},
		{"no spaces", "[0.1,0.2,0.3]", []float32{0.1, 0.2, 0.3}, false},
		{"whitespace", "  [ 0.1, 0.2 ] ", []float32{0.1, 0.2}, false},
		{"empty", "[]", []float32{}, false},
		{"empty content", "[ ]", []float32{}, false},
		{"extra commas", "[0.1,,0.2]", []float32{0.1, 0.2}, false},
		{"trailing comma", "[0.1,]", []float32{0.1}, false},
		{"negative", "[-0.5]", []float32{-0.5}, false},
		{"no brackets", "0.1, 0.2", nil, true},
		{"invalid num", "[abc]", nil, true},
		{"missing opening", "0.1, 0.2]", nil, true},
		{"missing closing", "[0.1, 0.2", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FastVectorParser(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				if len(got) != len(tt.expected) {
					t.Errorf("got length %d, want %d", len(got), len(tt.expected))
				}
				for i := range got {
					if math.Abs(float64(got[i]-tt.expected[i])) > 1e-6 {
						t.Errorf("got[%d] = %f, want %f", i, got[i], tt.expected[i])
					}
				}
			}
		})
	}
}

func TestRESPReaderComprehensive(t *testing.T) {
	t.Run("valid commands", func(t *testing.T) {
		inputs := []struct {
			data     string
			expected []string
		}{
			{"*1\r\n$4\r\nPING\r\n", []string{"PING"}},
			{"+OK\r\n", []string{"OK"}},
			{":100\r\n", []string{"100"}},
			{"$5\r\nhello\r\n", []string{"hello"}},
			{"$-1\r\n", []string{""}},
			{"+OK\r\n", []string{"OK"}},
		}
		for _, tt := range inputs {
			r := NewRESPReader(strings.NewReader(tt.data))
			cmd, err := r.ReadCommand()
			if err != nil {
				t.Errorf("unexpected error for %q: %v", tt.data, err)
				continue
			}
			if len(cmd) != 1 || cmd[0] != tt.expected[0] {
				t.Errorf("got %v, want %v", cmd, tt.expected)
			}
		}
	})

	t.Run("error paths", func(t *testing.T) {
		errors := []string{
			"",                  // EOF
			"X",                 // Invalid type
			"*\r\n",             // Short array
			"*abc\r\n",          // Invalid array len
			"*-5\r\n",           // Negative array len
			"$abc\r\n",          // Invalid bulk len
			"$-5\r\n",           // Negative bulk len
			"$5\r\nshrt\r\n",    // Short bulk
			"$5\r\nhelloXX",     // Missing CRLF bulk
			"+OK",               // Missing CRLF simple string
			"*1\r\n+OK",         // Missing CRLF nested
			"*1\r\n$3\r\nab",    // EOF mid bulk
			"*1\r\n$3\r\nabcXX", // Missing CRLF after bulk
			"*1\r\nX",           // Unexpected type in array
			"-ERR incomplete",   // Missing CRLF in error
			"+\r",               // Line too short
			"+\n",               // Line too short
		}
		for _, input := range errors {
			r := NewRESPReader(strings.NewReader(input))
			_, err := r.ReadCommand()
			if err == nil {
				t.Errorf("expected error for %q", input)
			}
		}
	})

	t.Run("readArray mid-failure", func(t *testing.T) {
		// First line succeeds, but then readValue fails
		r := NewRESPReader(strings.NewReader("*1\r\n"))
		_, err := r.ReadCommand()
		if err == nil {
			t.Error("expected error for array with missing element")
		}
	})

	t.Run("readValue error", func(t *testing.T) {
		r := NewRESPReader(strings.NewReader("-MYERR\r\n"))
		val, err := r.readValue()
		if val != "" || err == nil || err.Error() != "MYERR" {
			t.Errorf("expected error MYERR, got %v, %v", val, err)
		}
	})
}

type sequencedWriter struct {
	failAt int
	count  int
}

func (s *sequencedWriter) Write(p []byte) (int, error) {
	if s.failAt != -1 && s.count+len(p) > s.failAt {
		return 0, errors.New("injected failure")
	}
	s.count += len(p)
	return len(p), nil
}

func TestRESPWriterComprehensive(t *testing.T) {
	t.Run("standard writes", func(t *testing.T) {
		var buf bytes.Buffer
		w := NewRESPWriter(&buf)
		_ = w.WriteSimpleString("OK")
		_ = w.WriteError("fail")
		_ = w.WriteInteger(42)
		_ = w.WriteBulkString("hi")
		_ = w.WriteArray([]string{"a"})
		_ = w.Flush()
		expected := "+OK\r\n-ERR fail\r\n:42\r\n$2\r\nhi\r\n*1\r\n$1\r\na\r\n"
		if buf.String() != expected {
			t.Errorf("got %q, want %q", buf.String(), expected)
		}
	})

	t.Run("pinpoint failures", func(t *testing.T) {
		type testCase struct {
			name string
			fill int
			op   func(*RESPWriter) error
		}

		cases := []testCase{
			{"SimpleString_Call1", 4096, func(w *RESPWriter) error { return w.WriteSimpleString("OK") }},
			{"SimpleString_Call2", 4096 - 1, func(w *RESPWriter) error { return w.WriteSimpleString("OK") }},
			{"SimpleString_Call3", 4096 - 1 - 2, func(w *RESPWriter) error { return w.WriteSimpleString("OK") }},

			{"Error_Call1", 4096, func(w *RESPWriter) error { return w.WriteError("fail") }},
			{"Error_Call2", 4096 - 5, func(w *RESPWriter) error { return w.WriteError("fail") }},
			{"Error_Call3", 4096 - 5 - 4, func(w *RESPWriter) error { return w.WriteError("fail") }},

			{"Bulk_Call1", 4096, func(w *RESPWriter) error { return w.WriteBulkString("hi") }},
			{"Bulk_Call2", 4096 - 1, func(w *RESPWriter) error { return w.WriteBulkString("hi") }},
			{"Bulk_Call3", 4096 - 1 - 1, func(w *RESPWriter) error { return w.WriteBulkString("hi") }},
			{"Bulk_Call4", 4096 - 1 - 1 - 2, func(w *RESPWriter) error { return w.WriteBulkString("hi") }},
			{"Bulk_Call5", 4096 - 1 - 1 - 2 - 2, func(w *RESPWriter) error { return w.WriteBulkString("hi") }},

			{"Integer_Call1", 4096, func(w *RESPWriter) error { return w.WriteInteger(42) }},
			{"Integer_Call2", 4096 - 1, func(w *RESPWriter) error { return w.WriteInteger(42) }},
			{"Integer_Call3", 4096 - 1 - 2, func(w *RESPWriter) error { return w.WriteInteger(42) }},

			{"Array_Call1", 4096, func(w *RESPWriter) error { return w.WriteArray([]string{"a"}) }},
			{"Array_Call2", 4096 - 1, func(w *RESPWriter) error { return w.WriteArray([]string{"a"}) }},
			{"Array_Call3", 4096 - 1 - 1, func(w *RESPWriter) error { return w.WriteArray([]string{"a"}) }},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				sw := &sequencedWriter{failAt: 0}
				w := NewRESPWriter(sw)
				// Fill buffer manually
				_, _ = w.writer.WriteString(strings.Repeat("X", tc.fill))
				err := tc.op(w)
				if err == nil {
					t.Errorf("%s: expected error but got nil", tc.name)
				}
			})
		}
	})
}

type faultyReader struct {
	readErr error
}

func (f *faultyReader) Read(p []byte) (int, error) {
	return 0, f.readErr
}

func TestReaderLowLevel(t *testing.T) {
	t.Run("ReadByte fail", func(t *testing.T) {
		fr := &faultyReader{readErr: errors.New("fail")}
		r := NewRESPReader(fr)
		_, err := r.ReadCommand()
		if err == nil {
			t.Error("expected error")
		}
	})

	t.Run("readLine short no CRLF", func(t *testing.T) {
		r := NewRESPReader(strings.NewReader("A\n"))
		_, err := r.readLine()
		if err == nil || !errors.Is(err, ErrInvalidProtocol) {
			t.Errorf("expected ErrInvalidProtocol, got %v", err)
		}
	})
}

func BenchmarkFastVectorParser(b *testing.B) {
	input := "[0.1, 0.2, 0.3]"
	for i := 0; i < b.N; i++ {
		_, _ = FastVectorParser(input)
	}
}

func BenchmarkRESPReader(b *testing.B) {
	input := "*1\r\n$4\r\nPING\r\n"
	for i := 0; i < b.N; i++ {
		r := NewRESPReader(strings.NewReader(input))
		_, _ = r.ReadCommand()
	}
}
