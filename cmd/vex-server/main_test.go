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

package main

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/uzqw/vex/internal/protocol"
	"github.com/uzqw/vex/internal/search"
	"github.com/uzqw/vex/internal/storage"
	"github.com/uzqw/vex/pkg/logger"
)

// setupVSearch wires a ModeNone manager (linear scan over storage) into the
// package-level mgr used by the handlers, then seeds two vectors.
func setupVSearch(t *testing.T) {
	t.Helper()
	m, err := search.NewManager(storage.New(), search.Config{Mode: search.ModeNone})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	mgr = m
	if _, err := mgr.Set("a", []float32{1, 0}); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Set("b", []float32{0, 1}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SetMetadata("a", map[string]string{"color": "red"}); err != nil {
		t.Fatal(err)
	}
}

func runVSearch(t *testing.T, args ...string) string {
	t.Helper()
	var buf bytes.Buffer
	w := protocol.NewRESPWriter(&buf)
	log := logger.New(logger.Config{Format: logger.FormatText, Level: slog.LevelError})
	handleVSearch(log, w, append([]string{"VSEARCH"}, args...))
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestVSearchWithScores(t *testing.T) {
	setupVSearch(t)

	// Query [1,0]: "a" is identical (sim 1), "b" orthogonal (sim 0).
	out := runVSearch(t, "[1, 0]", "2", "WITHSCORES")
	if !strings.HasPrefix(out, "*4\r\n") {
		t.Fatalf("expected 4-element flat array, got %q", out)
	}
	if !strings.Contains(out, "$1\r\na\r\n") || !strings.Contains(out, "$1\r\nb\r\n") {
		t.Fatalf("expected keys a and b, got %q", out)
	}
	// a must carry similarity 1
	if !strings.Contains(out, "$1\r\na\r\n$1\r\n1\r\n") {
		t.Fatalf("expected a -> 1, got %q", out)
	}
}

func TestVSearchWithScoresAndFilter(t *testing.T) {
	setupVSearch(t)

	// WITHSCORES before FILTER: only "a" matches color=red.
	out := runVSearch(t, "[1, 0]", "2", "WITHSCORES", "FILTER", "color=red")
	if !strings.HasPrefix(out, "*2\r\n") || !strings.Contains(out, "$1\r\na\r\n$1\r\n1\r\n") {
		t.Fatalf("expected only a with score 1, got %q", out)
	}

	// FILTER before WITHSCORES: same result.
	out = runVSearch(t, "[1, 0]", "2", "FILTER", "color=red", "WITHSCORES")
	if !strings.HasPrefix(out, "*2\r\n") || !strings.Contains(out, "$1\r\na\r\n$1\r\n1\r\n") {
		t.Fatalf("expected only a with score 1, got %q", out)
	}
}

func TestVSearchWithoutScoresUnchanged(t *testing.T) {
	setupVSearch(t)

	out := runVSearch(t, "[1, 0]", "1")
	if out != "*1\r\n$1\r\na\r\n" {
		t.Fatalf("expected plain key array, got %q", out)
	}
}

func TestVSearchSyntaxErrors(t *testing.T) {
	setupVSearch(t)

	for _, args := range [][]string{
		{"[1, 0]", "2", "WITHSCORES", "WITHSCORES"},
		{"[1, 0]", "2", "BOGUS"},
		{"[1, 0]", "2", "FILTER"},                         // missing value
		{"[1, 0]", "2", "FILTER", "bad"},                  // no '='
		{"[1, 0]", "2", "FILTER", "=x"},                   // empty field
		{"[1, 0]", "2", "FILTER", "c=r", "FILTER", "c=b"}, // FILTER twice
	} {
		out := runVSearch(t, args...)
		if !strings.HasPrefix(out, "-") {
			t.Fatalf("args %v: expected error reply, got %q", args, out)
		}
	}
}
