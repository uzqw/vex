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
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/uzqw/vex/internal/protocol"
)

var (
	host        = flag.String("host", "localhost", "Server host")
	port        = flag.String("port", "6379", "Server port")
	concurrency = flag.Int("concurrency", 50, "Number of concurrent connections")
	totalOps    = flag.Int("n", 100000, "Total number of operations")
	mode        = flag.String("mode", "insert", "Benchmark mode: insert or search")
	dim         = flag.Int("dim", 128, "Vector dimension")
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
	flag.Parse()

	if *showVer {
		fmt.Printf("Vex benchmark version %s\n", Version)
		return
	}

	fmt.Println("=== Vex Benchmark ===")
	fmt.Printf("Mode:        %s\n", *mode)
	fmt.Printf("Host:        %s:%s\n", *host, *port)
	fmt.Printf("Concurrency: %d\n", *concurrency)
	fmt.Printf("Total Ops:   %d\n", *totalOps)
	fmt.Printf("Dimensions:  %d\n", *dim)
	fmt.Println("---")

	var result *BenchmarkResult
	switch *mode {
	case "insert":
		result = runInsertBenchmark()
	case "search":
		result = runSearchBenchmark()
	default:
		fmt.Printf("Unknown mode: %s\n", *mode)
		return
	}

	printResult(result)
}

func runInsertBenchmark() *BenchmarkResult {
	var wg sync.WaitGroup
	var successCount, errorCount atomic.Int64
	latencies := make([]time.Duration, *totalOps)
	opsPerWorker := *totalOps / *concurrency

	startTime := time.Now()

	for i := 0; i < *concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			// Create connection for this worker
			conn, err := net.Dial("tcp", net.JoinHostPort(*host, *port))
			if err != nil {
				errorCount.Add(int64(opsPerWorker))
				return
			}
			defer conn.Close()

			writer := protocol.NewRESPWriter(conn)
			reader := protocol.NewRESPReader(conn)

			for j := 0; j < opsPerWorker; j++ {
				idx := workerID*opsPerWorker + j
				key := fmt.Sprintf("vec:%d", idx)
				vector := generateRandomVector(*dim)

				opStart := time.Now()

				// Send VSET command
				cmd := []string{"VSET", key, formatVector(vector)}
				if err := sendCommand(writer, cmd); err != nil {
					errorCount.Add(1)
					continue
				}

				// Read response
				_, err := reader.ReadCommand()
				if err != nil {
					errorCount.Add(1)
					continue
				}

				latency := time.Since(opStart)
				latencies[idx] = latency
				successCount.Add(1)
			}
		}(i)
	}

	wg.Wait()
	totalTime := time.Since(startTime)

	return calculateResult(latencies, totalTime, successCount.Load(), errorCount.Load())
}

func runSearchBenchmark() *BenchmarkResult {
	// First, insert some vectors to search against
	fmt.Println("Preparing data for search benchmark...")
	prepareSearchData()

	var wg sync.WaitGroup
	var successCount, errorCount atomic.Int64
	latencies := make([]time.Duration, *totalOps)
	opsPerWorker := *totalOps / *concurrency

	startTime := time.Now()

	for i := 0; i < *concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			// Create connection for this worker
			conn, err := net.Dial("tcp", net.JoinHostPort(*host, *port))
			if err != nil {
				errorCount.Add(int64(opsPerWorker))
				return
			}
			defer conn.Close()

			writer := protocol.NewRESPWriter(conn)
			reader := protocol.NewRESPReader(conn)

			for j := 0; j < opsPerWorker; j++ {
				idx := workerID*opsPerWorker + j
				vector := generateRandomVector(*dim)

				opStart := time.Now()

				// Send VSEARCH command
				cmd := []string{"VSEARCH", formatVector(vector), "10"}
				if err := sendCommand(writer, cmd); err != nil {
					errorCount.Add(1)
					continue
				}

				// Read response
				_, err := reader.ReadCommand()
				if err != nil {
					errorCount.Add(1)
					continue
				}

				latency := time.Since(opStart)
				latencies[idx] = latency
				successCount.Add(1)
			}
		}(i)
	}

	wg.Wait()
	totalTime := time.Since(startTime)

	return calculateResult(latencies, totalTime, successCount.Load(), errorCount.Load())
}

func prepareSearchData() {
	conn, err := net.Dial("tcp", net.JoinHostPort(*host, *port))
	if err != nil {
		fmt.Printf("Failed to connect: %s\n", err)
		return
	}
	defer conn.Close()

	writer := protocol.NewRESPWriter(conn)
	reader := protocol.NewRESPReader(conn)

	// Insert 1000 vectors
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("vec:%d", i)
		vector := generateRandomVector(*dim)

		cmd := []string{"VSET", key, formatVector(vector)}
		if err := sendCommand(writer, cmd); err != nil {
			continue
		}
		_, _ = reader.ReadCommand()
	}

	fmt.Println("Data preparation complete.")
}

func sendCommand(writer *protocol.RESPWriter, cmd []string) error {
	if err := writer.WriteArray(cmd); err != nil {
		return err
	}
	return writer.Flush()
}

func generateRandomVector(dim int) []float32 {
	vec := make([]float32, dim)
	for i := 0; i < dim; i++ {
		vec[i] = rand.Float32()*2 - 1 // Random value between -1 and 1
	}
	return vec
}

func formatVector(vec []float32) string {
	result := "["
	for i, v := range vec {
		if i > 0 {
			result += ", "
		}
		result += fmt.Sprintf("%.6f", v)
	}
	result += "]"
	return result
}

func calculateResult(latencies []time.Duration, totalTime time.Duration, successCount, errorCount int64) *BenchmarkResult {
	// Filter out zero latencies (errors)
	validLatencies := make([]time.Duration, 0, successCount)
	for _, l := range latencies {
		if l > 0 {
			validLatencies = append(validLatencies, l)
		}
	}

	if len(validLatencies) == 0 {
		return &BenchmarkResult{
			TotalOps:     *totalOps,
			TotalTime:    totalTime,
			SuccessCount: successCount,
			ErrorCount:   errorCount,
		}
	}

	// Sort latencies for percentile calculation
	sort.Slice(validLatencies, func(i, j int) bool {
		return validLatencies[i] < validLatencies[j]
	})

	// Calculate statistics
	var totalLatency time.Duration
	for _, l := range validLatencies {
		totalLatency += l
	}

	n := len(validLatencies)
	result := &BenchmarkResult{
		TotalOps:     *totalOps,
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

	return result
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
