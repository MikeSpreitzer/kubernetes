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
	"strconv"
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
	ver     uint64
	defined bool
}

// UndefinedAsString is the string representation of "undefined"
const UndefinedAsString = ""

// AnyAsString is the string representation of "any"
const AnyAsString = "0"

// ParseResourceVersion parses the string representation
func ParseResourceVersion(rvS string) (ResourceVersion, error) {
	if rvS == "" {
		return ResourceVersion{}, nil
	}
	rvU, err := strconv.ParseUint(rvS, 10, 64)
	if err != nil {
		return ResourceVersion{}, err
	}
	return ResourceVersion{ver: rvU, defined: true}, nil
}

// IsUndefined tells whether this is the distinguished value
// that means "undefined".  It is the zero value, in go
// language terms, commonly used to mean that no particular
// value is being provided.
func (rv ResourceVersion) IsUndefined() bool {
	return !rv.defined
}

// IsAny tells whether this is the distinguished value that
// means "any".  This value never comes from a store, but can
// be used in a query to explicitly say that the requestor
// does not care which state is queried.
func (rv ResourceVersion) IsAny() bool {
	return rv.defined && rv.ver == 0
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
		return UndefinedAsString
	}
	return strconv.FormatUint(rv.ver, 10)
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
	}
	if rv.IsAny() {
		otherIsAny := other.IsAny()
		return Comparison{LE: otherIsAny, GE: otherIsAny}
	}
	if other.IsSpecial() {
		return InComparable()
	}
	if rv.ver <= other.ver {
		return Comparison{LE: true, GE: rv.ver == other.ver}
	} else {
		return Greater()
	}
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
	ans := rv
	if other.ver > ans.ver {
		ans.ver = other.ver
	}
	return ans
}
