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
	"strings"
)

// stringrv is an implementation of ResourceVersion that is based
// on string comparison, with special considerations that make
// it give the usual results when applied to the string representation
// of a uint64.
// The representation is a string, with the contraint that if the
// string begins with a decimal digit then the length of the string
// must be no more than 26.
// Comparison is done by effectively prepending a length indicator
// to strings that start with a decimal digit, leaving other strings
// unchanged.
type stringrv string

// ParseStringRV parses the given string
func ParseStringRV(str string) (ResourceVersion, error) {
	return parseStringRV(str)
}

func parseStringRV(str string) (stringrv, error) {
	if str != "" && str[0] >= '0' && str[0] <= '9' {
		slen := len(str)
		if slen > 26 {
			return "", fmt.Errorf("length (%d) is too long", slen)
		}
	}
	return stringrv(str), nil
}

func (rv stringrv) IsDefined() bool {
	return rv != ""
}

func (rv stringrv) IsAny() bool {
	return rv == "0"
}

func (rv stringrv) IsSpecial() bool {
	return rv.IsAny() || !rv.IsDefined()
}

func (rv stringrv) String() string {
	return string(rv)
}

func (rv stringrv) CanonicalString() string {
	if rv.IsSpecial() {
		return string(rv)
	}
	rvs := string(rv)
	if rv[0] >= '0' && rv[0] <= '9' {
		pfx := []byte{byte('A' + len(rv) - 1)}
		return string(pfx) + rvs
	}
	return rvs
}

const hexDigits = "0123456789ABCDEF"

func (rv stringrv) StringForFilename() string {
	if rv == "" {
		return "undefined"
	}
	rvBytes := []byte(rv)
	var bld strings.Builder
	for _, r := range rvBytes {
		if r >= '0' && r <= '9' ||
			r >= 'A' && r <= 'Z' ||
			r >= 'a' && r <= 'z' ||
			r == '.' || r == '-' || r == '_' {
			bld.WriteByte(r)
		} else {
			bld.WriteRune('%')
			bld.WriteByte(hexDigits[r/16])
			bld.WriteByte(hexDigits[r%16])
		}
	}
	return bld.String()
}

type stringrvPair struct {
	stringrv
	other stringrv
}

func (rv stringrv) Parse(otherS string) (PairedResourceVersion, error) {
	other, err := parseStringRV(otherS)
	if err != nil {
		return nil, err
	}
	return stringrvPair{stringrv: rv, other: other}, nil
}

func (rvp stringrvPair) Compare() Comparison {
	if !rvp.IsDefined() {
		otherIsUndef := !rvp.other.IsDefined()
		return Comparison{LE: otherIsUndef, GE: otherIsUndef}
	} else if rvp.IsAny() {
		otherIsAny := rvp.other.IsAny()
		return Comparison{LE: otherIsAny, GE: otherIsAny}
	} else if rvp.other.IsSpecial() {
		return InComparable()
	}
	r1, s1 := compareParts(rvp.stringrv)
	r2, s2 := compareParts(rvp.other)
	switch {
	case r1 < r2:
		return Comparison{LE: true}
	case r1 > r2:
		return Comparison{GE: true}
	case s1 < s2:
		return Comparison{LE: true}
	case s1 > s2:
		return Comparison{GE: true}
	default:
		return Comparison{LE: true, GE: true}
	}
}

func compareParts(rv stringrv) (byte, string) {
	if rv[0] >= '0' && rv[0] <= '9' {
		return byte('A' + len(rv) - 1), string(rv)
	}
	return rv[0], string(rv)[1:]
}

func (rvp stringrvPair) Union() ResourceVersion {
	cmp := rvp.Compare()
	if cmp.GE {
		return rvp.stringrv
	}
	return rvp.other
}
