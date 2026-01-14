#!/bin/bash

# Copyright 2025 uzqw
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# run-all.sh - Run all Vex benchmarks (unit + integration)
#
# Usage:
#   ./benchmarks/run-all.sh [options]
#
# Options:
#   -u, --unit-only         Run only unit benchmarks (skip integration)
#   -i, --integration-only  Run only integration benchmarks (skip unit)
#   -p, --profile           Generate CPU and memory profiles
#   -t, --benchtime DURATION  Benchmark duration (default: 3s)
#   -c, --cpu-list CPUs     CPU counts for benchmark (default: auto)
#   -h, --help              Show this help message

set -e

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Configuration
UNIT_ONLY=0
INTEGRATION_ONLY=0
GENERATE_PROFILES=0
BENCHMARK_TIME="3s"
CPU_LIST=""
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

# Functions
print_help() {
    cat << EOF
Usage: $0 [options]

Run Vex benchmarks (unit-level and integration-level performance tests)

Options:
  -u, --unit-only         Run only unit benchmarks (skip integration)
  -i, --integration-only  Run only integration benchmarks (skip unit)
  -p, --profile           Generate CPU and memory profiles
  -t, --benchtime DURATION  Benchmark duration (default: 3s)
  -c, --cpu-list CPUs     CPU counts for benchmark (default: 1,2,4,8,16)
  -h, --help              Show this help message

Examples:
  # Run all benchmarks
  $0

  # Run only unit benchmarks
  $0 --unit-only

  # Run with profiling
  $0 --profile

  # Run with longer benchmark time
  $0 --benchtime=10s

  # Run with custom CPU list
  $0 --cpu-list=1,4,8

Environment Variables:
  BENCHMARK_TIME    Override benchmark duration (e.g., BENCHMARK_TIME=5s $0)
  BENCHMARK_TIMEOUT Overall timeout for benchmarks (default: 30m)

EOF
}

print_section() {
    echo ""
    echo -e "${BLUE}==== $1 ====${NC}"
    echo ""
}

print_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

print_warning() {
    echo -e "${YELLOW}⚠ $1${NC}"
}

print_error() {
    echo -e "${RED}✗ $1${NC}"
}

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -u|--unit-only)
            UNIT_ONLY=1
            shift
            ;;
        -i|--integration-only)
            INTEGRATION_ONLY=1
            shift
            ;;
        -p|--profile)
            GENERATE_PROFILES=1
            shift
            ;;
        -t|--benchtime)
            BENCHMARK_TIME="$2"
            shift 2
            ;;
        -c|--cpu-list)
            CPU_LIST="$2"
            shift 2
            ;;
        -h|--help)
            print_help
            exit 0
            ;;
        *)
            print_error "Unknown option: $1"
            print_help
            exit 1
            ;;
    esac
done

# Check if neither or both flags are set
if [ $UNIT_ONLY -eq 1 ] && [ $INTEGRATION_ONLY -eq 1 ]; then
    print_error "Cannot use both --unit-only and --integration-only"
    exit 1
fi

# Default to both if neither specified
if [ $UNIT_ONLY -eq 0 ] && [ $INTEGRATION_ONLY -eq 0 ]; then
    RUN_UNIT=1
    RUN_INTEGRATION=1
else
    RUN_UNIT=$UNIT_ONLY
    RUN_INTEGRATION=$INTEGRATION_ONLY
fi

echo -e "${GREEN}Vex Benchmark Suite${NC}"
echo "Time: $(date)"
echo "System: $(uname -s) $(uname -m)"
echo "CPU Cores: $(nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo 'unknown')"
echo ""

# Run unit benchmarks
if [ $RUN_UNIT -eq 1 ]; then
    print_section "Unit Benchmarks (Micro-level Performance Tests)"

    cd "$PROJECT_ROOT"

    if [ -n "$CPU_LIST" ]; then
        print_warning "Running benchmarks with custom CPU list: $CPU_LIST"
        go test -bench=. -benchmem -benchtime="$BENCHMARK_TIME" \
            -cpu="$CPU_LIST" ./benchmarks/...
    else
        go test -bench=. -benchmem -benchtime="$BENCHMARK_TIME" ./benchmarks/...
    fi

    if [ $? -eq 0 ]; then
        print_success "Unit benchmarks completed successfully"
    else
        print_error "Unit benchmarks failed"
        exit 1
    fi

    if [ $GENERATE_PROFILES -eq 1 ]; then
        print_section "Generating CPU Profile"
        go test -bench=. -benchmem -benchtime="$BENCHMARK_TIME" \
            -cpuprofile=cpu.prof ./benchmarks/storage/
        print_success "CPU profile saved to cpu.prof"
        echo "View with: go tool pprof -http=:8080 cpu.prof"
        echo ""
    fi
fi

# Run integration benchmarks
if [ $RUN_INTEGRATION -eq 1 ]; then
    print_section "Integration Benchmarks (End-to-End System Tests)"

    # Check if server is running
    if ! nc -z localhost 6379 2>/dev/null; then
        print_warning "Vex server not running on localhost:6379"
        echo ""
        echo "Start the server in another terminal:"
        echo "  make run"
        echo "  # or"
        echo "  go run ./cmd/vex-server/main.go"
        echo ""
        read -p "Continue anyway? (y/N): " -n 1 -r
        echo
        if [[ ! $REPLY =~ ^[Yy]$ ]]; then
            echo "Skipping integration benchmarks"
            exit 0
        fi
    fi

    cd "$PROJECT_ROOT"

    echo "Running insert benchmark (100k operations)..."
    go run ./cmd/vex-benchmark/main.go \
        -mode=insert \
        -concurrency=50 \
        -n=100000 \
        -dim=128

    echo ""
    echo "Running search benchmark (50k operations)..."
    go run ./cmd/vex-benchmark/main.go \
        -mode=search \
        -concurrency=50 \
        -n=50000 \
        -dim=128

    if [ $? -eq 0 ]; then
        print_success "Integration benchmarks completed successfully"
    else
        print_error "Integration benchmarks failed"
        exit 1
    fi
fi

print_section "Benchmark Summary"
echo "All benchmarks completed successfully!"
echo ""
echo "Next steps:"
echo "  • Review results above"
echo "  • Compare with previous runs using: go run golang.org/x/perf/cmd/benchstat@latest"
echo "  • Profile performance: go tool pprof cpu.prof"
echo ""
