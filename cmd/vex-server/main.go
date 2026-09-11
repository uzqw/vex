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
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/uzqw/vex/internal/metrics"
	"github.com/uzqw/vex/internal/protocol"
	"github.com/uzqw/vex/internal/search"
	"github.com/uzqw/vex/internal/storage"
	"github.com/uzqw/vex/internal/storage/persistence"
	"github.com/uzqw/vex/pkg/logger"
)

const (
	defaultPort = "6379"
	defaultHost = "0.0.0.0"
)

var (
	host      = flag.String("host", defaultHost, "Host to bind to")
	port      = flag.String("port", defaultPort, "Port to listen on")
	logFormat = flag.String("log-format", "text", "Log format: text or json")
	logLevel  = flag.String("log-level", "info", "Log level: debug, info, warn, error")
	indexMode = flag.String("index", "none", "Search index: none, bruteforce, hnsw, or auto")
	autoMin   = flag.Int("auto-index-min-vectors", 10000, "Minimum vector count before auto mode uses HNSW search")
	hnswM     = flag.Int("hnsw-m", storage.DefaultHNSWM, "HNSW max neighbors per upper layer")
	hnswEf    = flag.Int("hnsw-ef", storage.DefaultHNSWEf, "HNSW search beam width")
	hnswEfC   = flag.Int("hnsw-ef-construction", storage.DefaultHNSWEfConstruct, "HNSW construction beam width")
	hnswSeed  = flag.Int64("hnsw-seed", 0, "HNSW RNG seed (0 uses random seed)")
	persist   = flag.Bool("persist", false, "Enable snapshot persistence (load on start, snapshot on interval and shutdown)")
	dataDir   = flag.String("data-dir", "./data", "Directory for persistence snapshots")
	snapSecs  = flag.Int("snapshot-secs", 300, "Seconds between automatic snapshots (0 disables scheduled snapshots)")
	showVer   = flag.Bool("version", false, "Show version and exit")
	store     *storage.Storage
	mgr       *search.Manager
	persMgr   *persistence.Manager
	log       *logger.Logger

	// Version is set at build time via ldflags
	Version = "dev"
)

func init() {
	// Customize usage output
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: vex-server [options]\n\n")
		fmt.Fprintf(os.Stderr, "Vex is a high-performance sharded in-memory vector database.\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nFor more information, visit https://github.com/uzqw/vex\n")
	}

	flag.Parse()

	// Handle version detection for 'go install'
	if Version == "dev" {
		if info, ok := debug.ReadBuildInfo(); ok {
			if info.Main.Version != "" && info.Main.Version != "(devel)" {
				Version = info.Main.Version
			}
		}
	}

	if *showVer {
		fmt.Printf("Vex server version %s\n", Version)
		os.Exit(0)
	}

	// Initialize logger
	level := slog.LevelInfo
	switch strings.ToLower(*logLevel) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	format := logger.FormatText
	if strings.ToLower(*logFormat) == "json" {
		format = logger.FormatJSON
	}

	log = logger.New(logger.Config{
		Format: format,
		Level:  level,
	})

	// Initialize storage + search manager
	store = storage.New()
	hnswFactory := func() storage.Index {
		return storage.NewHNSWIndexWithConfig(storage.HNSWConfig{
			M:           *hnswM,
			EfConstruct: *hnswEfC,
			Ef:          *hnswEf,
			Seed:        *hnswSeed,
		})
	}
	var cfg search.Config
	switch strings.ToLower(*indexMode) {
	case "none", "":
		cfg = search.Config{Mode: search.ModeNone}
	case "bruteforce":
		cfg = search.Config{
			Mode:     search.ModeBruteForce,
			NewIndex: func() storage.Index { return storage.NewBruteForceIndex() },
		}
	case "hnsw":
		cfg = search.Config{
			Mode:                   search.ModeHNSW,
			NewIndex:               hnswFactory,
			UseDefaultRebuildRatio: true,
		}
	case "auto":
		cfg = search.Config{
			Mode:                   search.ModeAuto,
			AutoMin:                *autoMin,
			NewIndex:               hnswFactory,
			UseDefaultRebuildRatio: true,
		}
	default:
		fmt.Fprintf(os.Stderr, "invalid -index value %q; expected none, bruteforce, hnsw, or auto\n", *indexMode)
		os.Exit(2)
	}
	var err error
	mgr, err = search.NewManager(store, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create search manager: %v\n", err)
		os.Exit(2)
	}

	// Persistence: snapshots read through mgr so HNSW/Auto index-held bodies
	// are captured (store.Get returns nil bodies for packed vectors).
	if *persist {
		pCfg := persistence.DefaultConfig()
		pCfg.Enabled = true
		pCfg.DataDir = *dataDir
		pCfg.SnapshotSeconds = *snapSecs
		snapshotter := persistence.NewVectorSnapshot(pCfg, mgr)
		persMgr, err = persistence.NewManager(pCfg, snapshotter)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to create persistence manager: %v\n", err)
			os.Exit(2)
		}
	}
}

func main() {
	addr := fmt.Sprintf("%s:%s", *host, *port)
	log.Info("starting Vex server", slog.String("addr", addr), slog.String("index", strings.ToLower(*indexMode)))

	// Start TCP listener
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Error("failed to start listener", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer func() { _ = listener.Close() }()

	log.Info("server started successfully", slog.String("addr", addr))

	// Setup graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Load latest snapshot and start the snapshot scheduler
	if persMgr != nil {
		if err := persMgr.Start(ctx); err != nil {
			log.Error("failed to load snapshot, starting empty", slog.String("error", err.Error()))
		}
	}

	// Handle OS signals for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		log.Info("received shutdown signal", slog.String("signal", sig.String()))
		cancel()
		_ = listener.Close()
	}()

	// Start memory monitoring goroutine
	go monitorMemory(ctx)

	// Accept connections
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				log.Info("shutting down server")
				if persMgr != nil {
					stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer stopCancel()
					if err := persMgr.TriggerSnapshot(stopCtx); err != nil {
						log.Error("final snapshot failed", slog.String("error", err.Error()))
					}
					_ = persMgr.Stop(stopCtx)
				}
				return
			default:
				log.Error("failed to accept connection", slog.String("error", err.Error()))
				continue
			}
		}

		// Handle connection in a new goroutine
		metrics.Global().IncrementActiveConnections()
		go handleConnection(ctx, conn)
	}
}

// handleConnection processes a single client connection
func handleConnection(ctx context.Context, conn net.Conn) {
	defer func() {
		_ = conn.Close()
		metrics.Global().DecrementActiveConnections()
	}()

	// Generate request ID for tracing
	requestID := uuid.New().String()
	connLog := log.WithRequestID(ctx, requestID)

	connLog.Info("new connection", slog.String("remote", conn.RemoteAddr().String()))

	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.SetNoDelay(true)
	}

	// Create RESP reader and writer
	reader := protocol.NewRESPReader(conn)
	writer := protocol.NewRESPWriter(conn)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Set read deadline to detect idle connections
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))

		// Read command
		cmd, err := reader.ReadCommand()
		if err != nil {
			// Check for normal connection closure (EOF means client disconnected)
			if errors.Is(err, net.ErrClosed) || errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) {
				connLog.Debug("connection closed")
				return
			}
			// Check for timeout
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				connLog.Info("connection timeout")
				return
			}
			// Protocol errors - log but try to send error response
			connLog.Warn("protocol error", slog.String("error", err.Error()))
			if writeErr := writer.WriteError(err.Error()); writeErr != nil {
				connLog.Debug("failed to write error response", slog.String("error", writeErr.Error()))
				return
			}
			if flushErr := writer.Flush(); flushErr != nil {
				connLog.Debug("failed to flush error response", slog.String("error", flushErr.Error()))
				return
			}
			// For protocol errors, close the connection to prevent further corruption
			return
		}

		if len(cmd) == 0 {
			continue
		}

		// Increment command counter
		metrics.Global().IncrementCommands()

		// Process command
		start := time.Now()
		processCommand(connLog, writer, cmd)
		latency := time.Since(start)

		// Log command execution
		connLog.Debug("command executed",
			slog.String("cmd", cmd[0]),
			slog.Int("args", len(cmd)-1),
			slog.Duration("latency", latency),
		)

		// Coalesce pipelined replies; last command in the kernel buffer still flushes.
		if reader.Buffered() == 0 {
			if err := writer.Flush(); err != nil {
				connLog.Error("failed to flush response", slog.String("error", err.Error()))
				return
			}
		}
	}
}

// processCommand handles individual commands
func processCommand(log *logger.Logger, writer *protocol.RESPWriter, cmd []string) {
	command := strings.ToUpper(cmd[0])

	switch command {
	case "PING":
		handlePing(writer, cmd)
	case "ECHO":
		handleEcho(writer, cmd)
	case "VSET":
		handleVSet(log, writer, cmd)
	case "VGET":
		handleVGet(writer, cmd)
	case "VDEL":
		handleVDel(writer, cmd)
	case "VSEARCH":
		handleVSearch(log, writer, cmd)
	case "STATS", "INFO":
		handleStats(writer)
	case "CLEAR":
		handleClear(writer)
	case "QUIT":
		_ = writer.WriteSimpleString("OK")
	default:
		_ = writer.WriteError(fmt.Sprintf("unknown command '%s'", command))
	}
}

// handlePing handles the PING command
func handlePing(writer *protocol.RESPWriter, cmd []string) {
	if len(cmd) == 1 {
		_ = writer.WriteSimpleString("PONG")
	} else {
		_ = writer.WriteBulkString(cmd[1])
	}
}

// handleEcho handles the ECHO command
func handleEcho(writer *protocol.RESPWriter, cmd []string) {
	if len(cmd) < 2 {
		_ = writer.WriteError("wrong number of arguments for 'echo' command")
		return
	}
	_ = writer.WriteBulkString(cmd[1])
}

// handleVSet handles the VSET command: VSET key "[0.1, 0.2, 0.3]"
func handleVSet(log *logger.Logger, writer *protocol.RESPWriter, cmd []string) {
	if len(cmd) < 3 {
		_ = writer.WriteError("wrong number of arguments for 'vset' command")
		return
	}

	key := cmd[1]
	vectorStr := cmd[2]

	// Parse vector
	values, err := protocol.FastVectorParser(vectorStr)
	if err != nil {
		_ = writer.WriteError(fmt.Sprintf("invalid vector format: %s", err.Error()))
		return
	}

	created, err := mgr.Set(key, values)
	if err != nil {
		_ = writer.WriteError(err.Error())
		return
	}

	if created {
		metrics.Global().IncrementKeys()
	}
	_ = writer.WriteSimpleString("OK")
}

// handleVGet handles the VGET command: VGET key
func handleVGet(writer *protocol.RESPWriter, cmd []string) {
	if len(cmd) < 2 {
		_ = writer.WriteError("wrong number of arguments for 'vget' command")
		return
	}

	key := cmd[1]
	values, ok := mgr.Get(key)
	if !ok {
		_ = writer.WriteBulkString("") // Null bulk string
		return
	}

	// Format vector as string
	var sb strings.Builder
	sb.WriteString("[")
	for i, v := range values {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(fmt.Sprintf("%.6f", v))
	}
	sb.WriteString("]")

	_ = writer.WriteBulkString(sb.String())
}

// handleVDel handles the VDEL command: VDEL key
func handleVDel(writer *protocol.RESPWriter, cmd []string) {
	if len(cmd) < 2 {
		_ = writer.WriteError("wrong number of arguments for 'vdel' command")
		return
	}

	key := cmd[1]
	deleted := mgr.Delete(key)
	if deleted {
		metrics.Global().DecrementKeys()
		_ = writer.WriteInteger(1)
	} else {
		_ = writer.WriteInteger(0)
	}
}

// handleVSearch handles the VSEARCH command: VSEARCH "[0.1, 0.2, 0.3]" k
func handleVSearch(log *logger.Logger, writer *protocol.RESPWriter, cmd []string) {
	if len(cmd) < 3 {
		_ = writer.WriteError("wrong number of arguments for 'vsearch' command")
		return
	}

	vectorStr := cmd[1]
	var k int
	_, _ = fmt.Sscanf(cmd[2], "%d", &k)

	if k <= 0 {
		_ = writer.WriteError("k must be positive")
		return
	}

	// Parse query vector
	query, err := protocol.FastVectorParser(vectorStr)
	if err != nil {
		_ = writer.WriteError(fmt.Sprintf("invalid vector format: %s", err.Error()))
		return
	}

	results, err := mgr.Search(query, k)
	if err != nil {
		_ = writer.WriteError(err.Error())
		return
	}

	// Format results as array of keys
	keys := make([]string, len(results))
	for i, res := range results {
		keys[i] = res.Key
	}

	_ = writer.WriteArray(keys)
}

// handleStats handles the STATS/INFO command
func handleStats(writer *protocol.RESPWriter) {
	jsonStr, err := metrics.Global().JSON()
	if err != nil {
		_ = writer.WriteError(err.Error())
		return
	}
	_ = writer.WriteBulkString(jsonStr)
}

// handleClear handles the CLEAR command
func handleClear(writer *protocol.RESPWriter) {
	mgr.Clear()
	_ = writer.WriteSimpleString("OK")
}

// monitorMemory periodically updates memory usage metrics
func monitorMemory(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			metrics.Global().SetMemoryUsage(m.Alloc)
		}
	}
}
