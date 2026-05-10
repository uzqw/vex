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
	"flag"
	"fmt"
	"math/rand"
	"net"
	"os"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/uzqw/vex/internal/protocol"
)

var (
	host        = flag.String("host", "localhost", "Server host")
	port        = flag.String("port", "6379", "Server port")
	concurrency = flag.Int("concurrency", 50, "Number of concurrent connections")
	totalOps    = flag.Int("n", 100000, "Total number of measured operations")
	mode        = flag.String("mode", "insert", "Benchmark mode: insert or search")
	dim         = flag.Int("dim", 128, "Vector dimension")
	prepareN    = flag.Int("prepare-n", 1000, "Number of vectors to load before search benchmarks")
	warmupOps   = flag.Int("warmup", 0, "Number of warmup operations to run before measuring")
	searchK     = flag.Int("k", 10, "Top-k value for VSEARCH")
	seed        = flag.Int64("seed", 42, "Random seed for deterministic vectors")
	keyPrefix   = flag.String("key-prefix", "vec", "Key prefix used for generated vectors")
	showVer     = flag.Bool("version", false, "Show version and exit")

	// Version is set at build time via ldflags
	Version = "dev"
)

type BenchmarkResult struct {
	TotalOps     int
	TotalTime    time.Duration
	QPS          float64
	AvgLatency   time.Duration
	P50Latency   time.Duration
	P95Latency   time.Duration
	P99Latency   time.Duration
	MinLatency   time.Duration
	MaxLatency   time.Duration
	SuccessCount int64
	ErrorCount   int64
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: vex-benchmark [options]\n\n")
		fmt.Fprintf(os.Stderr, "Vex Benchmark is a performance validation tool for the Vex vector database.\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	if Version == "dev" {
		if info, ok := debug.ReadBuildInfo(); ok {
			if info.Main.Version != "" && info.Main.Version != "(devel)" {
				Version = info.Main.Version
			}
		}
	}

	if *showVer {
		fmt.Printf("Vex benchmark version %s\n", Version)
		return
	}

	if err := validateFlags(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(2)
	}

	fmt.Println("=== Vex Benchmark ===")
	fmt.Printf("Mode:        %s\n", *mode)
	fmt.Printf("Host:        %s:%s\n", *host, *port)
	fmt.Printf("Concurrency: %d\n", *concurrency)
	fmt.Printf("Total Ops:   %d\n", *totalOps)
	fmt.Printf("Dimensions:  %d\n", *dim)
	fmt.Printf("K:           %d\n", *searchK)
	fmt.Printf("Prepare N:   %d\n", *prepareN)
	fmt.Printf("Warmup Ops:  %d\n", *warmupOps)
	fmt.Printf("Seed:        %d\n", *seed)
	fmt.Println("---")

	var result *BenchmarkResult
	switch strings.ToLower(*mode) {
	case "insert":
		result = runInsertBenchmark()
	case "search":
		result = runSearchBenchmark()
	default:
		fmt.Printf("Unknown mode: %s\n", *mode)
		os.Exit(2)
	}

	printResult(result)
}

func validateFlags() error {
	if *concurrency <= 0 {
		return fmt.Errorf("concurrency must be positive")
	}
	if *totalOps <= 0 {
		return fmt.Errorf("n must be positive")
	}
	if *dim <= 0 {
		return fmt.Errorf("dim must be positive")
	}
	if *searchK <= 0 {
		return fmt.Errorf("k must be positive")
	}
	if *prepareN < 0 {
		return fmt.Errorf("prepare-n cannot be negative")
	}
	if *warmupOps < 0 {
		return fmt.Errorf("warmup cannot be negative")
	}
	return nil
}

func runInsertBenchmark() *BenchmarkResult {
	if *warmupOps > 0 {
		fmt.Printf("Running %d warmup insert operations...\n", *warmupOps)
		runWorkload("insert", *warmupOps, false)
	}
	return runWorkload("insert", *totalOps, true)
}

func runSearchBenchmark() *BenchmarkResult {
	fmt.Printf("Preparing %d vectors for search benchmark...\n", *prepareN)
	prepareSearchData()
	if *warmupOps > 0 {
		fmt.Printf("Running %d warmup search operations...\n", *warmupOps)
		runWorkload("search", *warmupOps, false)
	}
	return runWorkload("search", *totalOps, true)
}

func runWorkload(workload string, ops int, recordLatency bool) *BenchmarkResult {
	var wg sync.WaitGroup
	var successCount, errorCount atomic.Int64
	var next atomic.Int64
	latencies := make([]time.Duration, ops)

	startTime := time.Now()
	for workerID := 0; workerID < *concurrency; workerID++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			conn, err := net.Dial("tcp", net.JoinHostPort(*host, *port))
			if err != nil {
				errorCount.Add(1)
				return
			}
			defer func() { _ = conn.Close() }()

			writer := protocol.NewRESPWriter(conn)
			reader := protocol.NewRESPReader(conn)
			rng := rand.New(rand.NewSource(*seed + int64(workerID)*7919 + int64(ops)))

			for {
				idx := int(next.Add(1) - 1)
				if idx >= ops {
					return
				}

				cmd := buildCommand(workload, idx, rng)
				opStart := time.Now()

				if err := sendCommand(writer, cmd); err != nil {
					errorCount.Add(1)
					continue
				}
				if _, err := reader.ReadCommand(); err != nil {
					errorCount.Add(1)
					continue
				}

				if recordLatency {
					latencies[idx] = time.Since(opStart)
				}
				successCount.Add(1)
			}
		}(workerID)
	}
	wg.Wait()

	totalTime := time.Since(startTime)
	if !recordLatency {
		return nil
	}
	return calculateResult(latencies, ops, totalTime, successCount.Load(), errorCount.Load())
}

func buildCommand(workload string, idx int, rng *rand.Rand) []string {
	vector := generateRandomVector(*dim, rng)
	switch workload {
	case "insert":
		key := fmt.Sprintf("%s:%d", *keyPrefix, idx)
		return []string{"VSET", key, formatVector(vector)}
	case "search":
		return []string{"VSEARCH", formatVector(vector), fmt.Sprintf("%d", *searchK)}
	default:
		panic(fmt.Sprintf("unknown workload %q", workload))
	}
}

func prepareSearchData() {
	if *prepareN == 0 {
		fmt.Println("Data preparation skipped.")
		return
	}

	conn, err := net.Dial("tcp", net.JoinHostPort(*host, *port))
	if err != nil {
		fmt.Printf("Failed to connect: %s\n", err)
		return
	}
	defer func() { _ = conn.Close() }()

	writer := protocol.NewRESPWriter(conn)
	reader := protocol.NewRESPReader(conn)
	rng := rand.New(rand.NewSource(*seed))

	var inserted, errors int
	for i := 0; i < *prepareN; i++ {
		key := fmt.Sprintf("%s:prepare:%d", *keyPrefix, i)
		vector := generateRandomVector(*dim, rng)

		cmd := []string{"VSET", key, formatVector(vector)}
		if err := sendCommand(writer, cmd); err != nil {
			errors++
			continue
		}
		if _, err := reader.ReadCommand(); err != nil {
			errors++
			continue
		}
		inserted++
	}

	fmt.Printf("Data preparation complete: inserted=%d errors=%d.\n", inserted, errors)
}

func sendCommand(writer *protocol.RESPWriter, cmd []string) error {
	if err := writer.WriteArray(cmd); err != nil {
		return err
	}
	return writer.Flush()
}

func generateRandomVector(dim int, rng *rand.Rand) []float32 {
	vec := make([]float32, dim)
	for i := range vec {
		vec[i] = rng.Float32()*2 - 1
	}
	return vec
}

func formatVector(vec []float32) string {
	var b strings.Builder
	b.Grow(len(vec) * 10)
	b.WriteByte('[')
	for i, v := range vec {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(fmt.Sprintf("%.6f", v))
	}
	b.WriteByte(']')
	return b.String()
}

func calculateResult(latencies []time.Duration, totalOps int, totalTime time.Duration, successCount, errorCount int64) *BenchmarkResult {
	validLatencies := make([]time.Duration, 0, successCount)
	for _, l := range latencies {
		if l > 0 {
			validLatencies = append(validLatencies, l)
		}
	}

	if len(validLatencies) == 0 {
		return &BenchmarkResult{
			TotalOps:     totalOps,
			TotalTime:    totalTime,
			SuccessCount: successCount,
			ErrorCount:   errorCount,
		}
	}

	sort.Slice(validLatencies, func(i, j int) bool {
		return validLatencies[i] < validLatencies[j]
	})

	var totalLatency time.Duration
	for _, l := range validLatencies {
		totalLatency += l
	}

	n := len(validLatencies)
	return &BenchmarkResult{
		TotalOps:     totalOps,
		TotalTime:    totalTime,
		QPS:          float64(successCount) / totalTime.Seconds(),
		AvgLatency:   totalLatency / time.Duration(n),
		P50Latency:   validLatencies[n*50/100],
		P95Latency:   validLatencies[min(n*95/100, n-1)],
		P99Latency:   validLatencies[min(n*99/100, n-1)],
		MinLatency:   validLatencies[0],
		MaxLatency:   validLatencies[n-1],
		SuccessCount: successCount,
		ErrorCount:   errorCount,
	}
}

func printResult(result *BenchmarkResult) {
	fmt.Println()
	fmt.Println("=== Benchmark Results ===")
	fmt.Printf("Total Time:    %v\n", result.TotalTime)
	fmt.Printf("QPS:           %.0f ops/sec\n", result.QPS)
	fmt.Printf("Success:       %d\n", result.SuccessCount)
	fmt.Printf("Errors:        %d\n", result.ErrorCount)
	fmt.Println()
	fmt.Println("Latency Statistics:")
	fmt.Printf("  Min:         %v\n", result.MinLatency)
	fmt.Printf("  Avg:         %v\n", result.AvgLatency)
	fmt.Printf("  P50:         %v\n", result.P50Latency)
	fmt.Printf("  P95:         %v\n", result.P95Latency)
	fmt.Printf("  P99:         %v\n", result.P99Latency)
	fmt.Printf("  Max:         %v\n", result.MaxLatency)
}
