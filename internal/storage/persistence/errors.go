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

package persistence

import "errors"

var (
	// ErrInvalidConfig indicates invalid persistence configuration
	ErrInvalidConfig = errors.New("invalid persistence configuration")

	// ErrSnapshotNotFound indicates no snapshot file was found
	ErrSnapshotNotFound = errors.New("snapshot not found")

	// ErrCorruptedSnapshot indicates the snapshot file is corrupted
	ErrCorruptedSnapshot = errors.New("snapshot is corrupted")

	// ErrChecksumMismatch indicates checksum verification failed
	ErrChecksumMismatch = errors.New("checksum mismatch")

	// ErrInvalidFormat indicates the snapshot format is invalid
	ErrInvalidFormat = errors.New("invalid snapshot format")

	// ErrVersionMismatch indicates incompatible snapshot version
	ErrVersionMismatch = errors.New("snapshot version mismatch")

	// ErrSnapshotInProgress indicates a snapshot is already running
	ErrSnapshotInProgress = errors.New("snapshot already in progress")
)
