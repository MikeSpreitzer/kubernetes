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
	// This representation is an attempt at handling a sharded
	// store in a way that supports growing (by shard fission) and
	// shrinking (but only by undoing a fission) the set of shards
	// and also supports transition between sharded and
	// non-sharded operation.
	//
	// A ResourceVersion is a tree in which each leaf is labeled
	// with a uint64 version number and each edge is labeled with
	// a string.
	//
	// The "undefined" value is a one-vertex tree with version
	// number zero.  The "any" value is a one-edge tree with the
	// edge labeled with "=" (which can not appear as a legitimate
	// edge label) and the one leaf having version zero.
	//
	// Having excluded the special values, branches compare
	// as follows.
	// Branch A >= branch B iff either:
	// - A is a leaf whose version number is >= every leaf
	//   version number in B, or
	// - B is a leaf whose version number is <= every leaf
	//   version number in A, or
	// - neither A nor B is a leaf and for every child CB of B
	//   there is a corresponding child CA of A and CA >= CB.
	//
	// The string syntax for non-special values is described by
	// the following grammar.
	// tree ::= simpletree | children
	// simpletree ::= uint64
	// children ::= child [ "," children ]
	// child ::= label "=" nonroot
	// nonroot ::= simpletree | "(" children ")"
	root vBranch
}

// vBranch is a branch in a version tree.
// Leaves have version numbers; the `version` in
// a non-leaf vertex is meaningless.
type vBranch struct {
	// children is the children of this vertex,
	// ordered by increasing label.
	// This vertex is a leaf iff it has no children.
	children vChildren
	version  uint64
}

type vChildren []vEdge

type vEdge struct {
	label string
	child vBranch
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
		return anyRV(), nil
	}
	rem, root, err := parseBranch(rvS)
	if err == nil && len(rem) > 0 {
		err = fmt.Errorf("junk at end: %q", rem)
	}
	return ResourceVersion{root: root}, err
}

// parseBranch parses a branch and returns the remainder of the input.
// It accepts a slightly more liberal grammar than is output,
// accepting parentheses around any branch.
func parseBranch(rem string) (string, vBranch, error) {
	idx := strings.IndexAny(rem, "=,()")
	if idx == 0 && rem[idx] == '(' {
		rem2, vbr, err := parseBranch(rem[1:])
		if err != nil {
			return rem2, vbr, fmt.Errorf("in parens: %w", err)
		}
		if len(rem2) < 1 || rem2[0] != ')' {
			return rem2, vbr, fmt.Errorf("missing close paren in %q", rem)
		}
		return rem2[1:], vbr, nil
	}
	if idx >= 0 && rem[idx] == '=' { // input is `children` possibly followed by ")" and later stuff
		kids := make(vChildren, 0, 3)
		for {
			label := rem[:idx]
			rem2, child, err := parseBranch(rem[idx+1:])
			if err != nil {
				return rem2, vBranch{children: kids}, fmt.Errorf("at %q: %w", label, err)
			}
			kids = append(kids, vEdge{label: label, child: child})
			if rem2 == "" || rem2[0] == ')' {
				sort.Sort(kids)
				return rem2, vBranch{children: kids}, nil
			}
			if rem2[0] != ',' {
				return rem2, vBranch{children: kids}, fmt.Errorf("no comma after %q=%v, rem2=%q", label, child, rem2)
			}
			rem = rem2[1:]
			if rem == "" {
				return rem, vBranch{children: kids}, fmt.Errorf("terminal comma on children")
			}
			idx = strings.IndexRune(rem, '=')
			if idx < 0 {
				return rem, vBranch{children: kids}, fmt.Errorf("no '=' in remainder %q", rem)
			}
		}
	} else { // rem, up to next special char, matches terminal uit64
		fin := idx
		if idx < 0 {
			fin = len(rem)
		}
		version, err := strconv.ParseUint(rem[:fin], 10, 64)
		if err != nil {
			return rem[fin:], vBranch{}, fmt.Errorf("uint64 syntax error in %q: %w", rem[:fin], err)
		}
		return rem[fin:], vBranch{version: version}, nil
	}
}

// IsUndefined tells whether this is the distinguished value
// that means "undefined".  It is the zero value, in go
// language terms, commonly used to mean that no particular
// value is being provided.
func (rv ResourceVersion) IsUndefined() bool {
	return rv.root.version == 0 && len(rv.root.children) == 0
}

// IsAny tells whether this is the distinguished value that
// means "any".  This value never comes from a store, but can
// be used in a query to explicitly say that the requestor
// does not care which state is queried.
func (rv ResourceVersion) IsAny() bool {
	if rv.root.version != 0 || len(rv.root.children) != 1 {
		return false
	}
	link0 := rv.root.children[0]
	return link0.label == "="
}

func anyRV() ResourceVersion {
	return ResourceVersion{root: vBranch{children: vChildren{vEdge{label: "="}}}}
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
	rv.root.writeToBuilder(&sb, false)
	return sb.String()
}

func (vbr vBranch) writeToBuilder(sb *strings.Builder, nested bool) {
	if len(vbr.children) == 0 {
		sb.WriteString(strconv.FormatUint(vbr.version, 10))
		return
	}
	if nested {
		sb.WriteRune('(')
	}
	for idx, edge := range vbr.children {
		if idx > 0 {
			sb.WriteRune(',')
		}
		sb.WriteString(edge.label)
		sb.WriteRune('=')
		edge.child.writeToBuilder(sb, true)
	}
	if nested {
		sb.WriteRune(')')
	}
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
	return rv.root.compareBranch(other.root)
}

func (vbr vBranch) compareBranch(other vBranch) Comparison {
	l1 := len(vbr.children)
	if l1 == 0 {
		return other.compareScalar(vbr.version).Reverse()
	}
	l2 := len(other.children)
	if l2 == 0 {
		return vbr.compareScalar(other.version)
	}
	ans := Equal()
	var i1, i2 int
	for i1 < l1 && i2 < l2 {
		if vbr.children[i1].label < other.children[i2].label {
			ans.LE = false
			if !ans.GE {
				break
			}
			i1++
		} else if vbr.children[i1].label > other.children[i2].label {
			ans.GE = false
			if !ans.LE {
				break
			}
			i2++
		} else {
			ans = vbr.children[i1].child.compareBranch(other.children[i2].child).And(ans)
			if !ans.IsComparable() {
				break
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

func (vbr vBranch) compareScalar(other uint64) Comparison {
	if len(vbr.children) == 0 {
		return compareUint64(vbr.version, other)
	}
	l1 := len(vbr.children)
	ans := Equal()
	for i1 := 0; i1 < l1; i1++ {
		ans = vbr.children[i1].child.compareScalar(other).And(ans)
		if !ans.IsComparable() {
			break
		}
	}
	return ans
}

// Union combines this and the given ResourceVersion to produce
// the canonical value that identifies the union of the two histories.
// Combining with Unspecified or Any is the identity function.
//
// When comparing two non-special ResourceVersion values,
// and the recusion comes down to comparing a leaf with a non-leaf,
// this operation is only valid if the two are comparable (i.e., one
// follows the other in the history of the sharding topology).
func (rv ResourceVersion) Union(other ResourceVersion) (ResourceVersion, error) {
	if rv.IsSpecial() {
		return other, nil
	}
	if other.IsSpecial() {
		return rv, nil
	}
	root, err := rv.root.unionBranch(other.root)
	return ResourceVersion{root: root}, err
}

func (vbr vBranch) unionBranch(other vBranch) (vBranch, error) {
	l1 := len(vbr.children)
	if l1 == 0 {
		return other.unionScalar(vbr.version)
	}
	l2 := len(other.children)
	if l2 == 0 {
		return vbr.unionScalar(other.version)
	}
	ans := make(vChildren, 0, l1)
	var i1, i2 int
	for i1 < l1 || i2 < l2 {
		if i1 < l1 && (i2 == l2 || vbr.children[i1].label < other.children[i2].label) {
			ans = append(ans, vbr.children[i1])
			i1++
		} else if i2 < l2 && (i1 == l1 || vbr.children[i1].label > other.children[i2].label) {
			ans = append(ans, other.children[i2])
			i2++
		} else {
			label := vbr.children[i1].label
			uChild, err := vbr.children[i1].child.unionBranch(other.children[i2].child)
			if err != nil {
				return vBranch{children: ans}, fmt.Errorf("at %q: %w", label, err)
			}
			ans = append(ans, vEdge{label: label, child: uChild})
			i1++
			i2++
		}
	}
	return vBranch{children: ans}, nil
}

func (vbr vBranch) unionScalar(other uint64) (vBranch, error) {
	comp := vbr.compareScalar(other)
	if comp.IsLessOrEqual() {
		return vBranch{version: other}, nil
	}
	if comp.IsGreater() {
		return vbr, nil
	}
	return vbr, fmt.Errorf("impossible union between %q and version %v", vbr, other)
}

func (vv vChildren) Len() int { return len(vv) }

func (vv vChildren) Less(i, j int) bool { return vv[i].label < vv[j].label }

func (vv vChildren) Swap(i, j int) { vv[i], vv[j] = vv[j], vv[i] }

func compareUint64(a, b uint64) Comparison {
	if a <= b {
		return Comparison{LE: true, GE: a == b}
	} else {
		return Greater()
	}
}
