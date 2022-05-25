/*
Copyright 2022 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package semanticrv

// ResourceVersion nominally identifies a closed set of write transactions
// on an API service and thus is uniquely defined for the state
// those transactions collectively produce.
// Closure means that for every included write X,
// every write W that happens-before X is also included.
// There are two special values of ResourceVersion,
// which do not identify a set of transactions but are used
// by a client in a position that _may_ specify a set of transactions; see
// https://kubernetes.io/docs/reference/using-api/api-concepts/#the-resourceversion-parameter
// for the two special values and their meaning.
type ResourceVersion interface {

	// IsDefined returns `false` for the special value that clients use when they do not intend to say anything
	IsDefined() bool

	// IsAny returns `true` for the special value that clients use to say that any set of transactions is acceptable
	IsAny() bool

	// IsSpecial returns `true` for either of the special values
	IsSpecial() bool

	// String returns a string representation, not necessarily the only equivalent one
	String() string

	// CanonicalString is like String but returns the same value for all equivalent receivers
	CanonicalString() string

	// StringForFilename returns a string that is safe for inclusion in filenames.
	// The result has no runes that are metacharacters for POSIX or Windows filesystems.
	StringForFilename() string

	// Parse parses another ResourceVersion and returns it, in a form implicitly paired
	// with the receiver for the binary operations of PairedResourceVersion.
	Parse(string) (PairedResourceVersion, error)
}

// PairedResourceVersion extends ResourceVersion with binary operations
// involving an implicit other ResourceVersion having the same implementation.
type PairedResourceVersion interface {
	ResourceVersion

	// Compare returns the comparison of this ResourceVersion (on the left side) with
	// the implicit other ResourceVersion (on the right side).
	// It is conceivable that the two ResourceVersion values are incomparable,
	// for example when the storage is sharded.
	// In a kube apiserver WATCH operation that starts from a specific ResourceVersion V0
	// and delivers notifications with ResourceVersions V1, V2, ..., the server promises
	// V0 < V1 < v2 ...
	Compare() Comparison

	// Union returns a ResourceVersion that represents the union of the two input
	// write transaction sets.
	Union() ResourceVersion
}
