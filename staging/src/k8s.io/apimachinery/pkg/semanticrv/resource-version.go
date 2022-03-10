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

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// ResourceVersion has public methods that define the semantics that
// can be expected from an ObjectMeta.ResourceVersion.  A
// ResourceVersion identifies the amount of history reflected in a
// state of a store, or (more generally) a query parameter that can
// optionally be used to constrain that.
//
// There are two special values of ResourceVersion that never identify
// a store's history, they are only used as values of a query
// parameter; we name them "undefined" and "any".  See
// [https://kubernetes.io/docs/reference/using-api/api-concepts/#the-resourceversion-parameter]
// for a discussion of the special query parameter values.
type ResourceVersion struct {
	// This representation is a simple attempt at handling
	// a sharded store.
	// It handles adding shards naturally.
	// It does not behave properly when a shard is retired,
	// assuming that a retired shard no longer appears in the
	// representation.
	// This representation also does not attempt to handle
	// a transition from a non-sharded store to a sharded one.
	//
	// The string syntax is a series of entries separated by ',',
	// where each entry consists of a block identifier, an '=',
	// then a version number (uint64 in base 10) for that block.
	// The entries must be in ascending order of block ID.
	// A block not mentioned is considered to have empty history.
	// The "undefined" value is the zero value, a nil slice.
	// The "any" value is represented by a slice with just one entry,
	// that having block ID "" and version number 0.
	vv versionVector
}

type versionVector []vvEntry

type vvEntry struct {
	block   string
	version uint64
}

// UndefinedAsString is the string representation of "undefined"
const UndefinedAsString = ""

// AnyAsString is the string representation of "any"
const AnyAsString = "0"

// ParseResourceVersion parses the string representation
func ParseResourceVersion(rvS string) (ResourceVersion, error) {
	if rvS == UndefinedAsString {
		return ResourceVersion{}, nil
	}
	if rvS == AnyAsString {
		return ResourceVersion{vv: versionVector{anyEntry()}}, nil
	}
	ans := ResourceVersion{vv: make(versionVector, 0, len(rvS)/8)}
	for rem, remLen := rvS, len(rvS); remLen > 0; {
		entrySepIdx := strings.IndexRune(rem, ',')
		var remNext string
		if entrySepIdx < 0 {
			entrySepIdx = remLen
		} else {
			remNext = rem[entrySepIdx+1:]
		}
		entry := rem[:entrySepIdx]
		bindIdx := strings.IndexRune(entry, '=')
		if bindIdx < 0 {
			return ans, fmt.Errorf("ResourceVersion %q has no equals in entry %q", rvS, entry)
		}
		version, err := strconv.ParseUint(entry[bindIdx+1:], 10, 64)
		if err != nil {
			return ans, fmt.Errorf("ResourceVersion %q has malformed version number in entry %q: %w", rvS, entry, err)
		}
		ans.vv = append(ans.vv, vvEntry{block: entry[:bindIdx], version: version})
		rem = remNext
		remLen = len(rem)
	}
	sort.Sort(ans.vv)
	return ans, nil
}

func anyEntry() vvEntry {
	return vvEntry{block: "", version: 0}
}

// IsUndefined tells whether this is the distinguished value
// that means "undefined".  It is the zero value, in go
// language terms, commonly used to mean that no particular
// value is being provided.
func (rv ResourceVersion) IsUndefined() bool {
	return rv.vv == nil
}

// IsAny tells whether this is the distinguished value that
// means "any".  This value never comes from a store, but can
// be used in a query to explicitly say that the requestor
// does not care which state is queried.
func (rv ResourceVersion) IsAny() bool {
	return len(rv.vv) == 1 && rv.vv[0] == anyEntry()
}

// IsSpecial reveals whether the given ResourceVersion is one of the special
// values that can not come from a store.
func (rv ResourceVersion) IsSpecial() bool {
	return rv.IsUndefined() || rv.IsAny()
}

// String returns the canonical representation to appear in
// ObjectMeta.ResourceVersion
func (rv ResourceVersion) String() string {
	if rv.IsUndefined() {
		return ""
	}
	if rv.IsAny() {
		return "0"
	}
	var sb strings.Builder
	for idx, vve := range rv.vv {
		if idx > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(vve.block)
		sb.WriteRune('=')
		sb.WriteString(strconv.FormatUint(vve.version, 10))
	}
	return sb.String()
}

// StringForFilename returns an encoded representation
// suitable for concatenation into filenames on Windows and
// Linux.
// Does not include directory separator (forward or back slash).
func (rv ResourceVersion) StringForFilename() string {
	if rv.IsUndefined() {
		return "undefined"
	}
	return rv.String()
}

// Compare this with another given ResourceVersion.
// Unspecified and Any are incomparable with other values.
func (rv ResourceVersion) Compare(other ResourceVersion) Comparison {
	if rv.IsUndefined() {
		otherIsUndef := other.IsUndefined()
		return Comparison{LE: otherIsUndef, GE: otherIsUndef}
	} else if rv.IsAny() {
		otherIsAny := other.IsAny()
		return Comparison{LE: otherIsAny, GE: otherIsAny}
	} else if other.IsSpecial() {
		return InComparable()
	}
	l1 := len(rv.vv)
	l2 := len(other.vv)
	ans := Equal()
	var i1, i2 int
	for i1 < l1 && i2 < l2 {
		if rv.vv[i1].block < other.vv[i2].block {
			ans.LE = false
			if !ans.GE {
				break
			}
			i1++
		} else if rv.vv[i1].block > other.vv[i2].block {
			ans.GE = false
			if !ans.LE {
				break
			}
			i2++
		} else {
			if rv.vv[i1].version < other.vv[i2].version {
				ans.GE = false
				if !ans.LE {
					break
				}
			} else if rv.vv[i1].version > other.vv[i2].version {
				ans.LE = false
				if !ans.GE {
					break
				}
			}
			i1++
			i2++
		}
	}
	if i1 < l1 {
		ans.LE = false
	} else if i2 < l2 {
		ans.GE = false
	}
	return ans
}

// Union combines this and the given ResourceVersion to produce
// the canonical value that identifies the union of the two histories.
// Combining with Unspecified or Any is the identity function.
func (rv ResourceVersion) Union(other ResourceVersion) ResourceVersion {
	if rv.IsSpecial() {
		return other
	}
	if other.IsSpecial() {
		return rv
	}
	l1 := len(rv.vv)
	l2 := len(other.vv)
	ans := make(versionVector, 0, l1)
	var i1, i2 int
	for i1 < l1 || i2 < l2 {
		if i1 < l1 && (i2 == l2 || rv.vv[i1].block < other.vv[i2].block) {
			ans = append(ans, rv.vv[i1])
			i1++
		} else if i2 < l2 && (i1 == l1 || rv.vv[i1].block > other.vv[i2].block) {
			ans = append(ans, other.vv[i2])
			i2++
		} else {
			if rv.vv[i1].version < other.vv[i2].version {
				ans = append(ans, other.vv[i2])
			} else {
				ans = append(ans, rv.vv[i1])
			}
			i1++
			i2++
		}
	}
	return ResourceVersion{vv: ans}
}

func (vv versionVector) Len() int { return len(vv) }

func (vv versionVector) Less(i, j int) bool { return vv[i].block < vv[j].block }

func (vv versionVector) Swap(i, j int) { vv[i], vv[j] = vv[j], vv[i] }
