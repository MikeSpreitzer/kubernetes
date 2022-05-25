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
	"testing"
)

func TestEm(t *testing.T) {
	anUndef, _ := ParseRVector(UndefinedAsString)
	anAny, _ := ParseRVector(AnyAsString)
	for _, testCase := range []struct {
		str          string
		parseErr     bool
		isDef        bool
		isAny        bool
		canonicalStr string
	}{
		{"xyz", true, true, false, ""},
		{UndefinedAsString, false, false, false, ""},
		{AnyAsString, false, true, true, ""},
		{"a=2", false, true, false, ""},
		{"b=10", false, true, false, ""},
		{"a=2,b=10", false, true, false, ""},
		{"b=10,a=2", false, true, false, "a=2,b=10"},
	} {
		parsed, err := ParseRVector(testCase.str)
		if (err != nil) != testCase.parseErr {
			t.Errorf("Parse of %q produced error %#+v", testCase.str, err)
		}
		if err != nil {
			continue
		}
		if a, e := parsed.IsDefined(), testCase.isDef; a != e {
			t.Errorf("%q.IsDefined()==%v but expected %v", testCase.str, a, e)
		}
		if a, e := parsed.IsAny(), testCase.isAny; a != e {
			t.Errorf("%q.IsAny()==%v but expected %v", testCase.str, a, e)
		}
		canStr := testCase.str
		if len(testCase.canonicalStr) > 0 {
			canStr = testCase.canonicalStr
		}
		if a, e := parsed.String(), canStr; a != e {
			t.Errorf("%q.String()==%q", e, a)
		}
		efile := canStr
		if efile == UndefinedAsString {
			efile = "undefined"
		}
		if a := parsed.StringForFilename(); a != efile {
			t.Errorf("%q.String()==%q", testCase.str, a)
		}
		withUndef, err := parsed.Parse(UndefinedAsString)
		if err != nil {
			t.Error(err)
		}
		if a, e := withUndef.Compare(), c(!parsed.IsDefined(), !parsed.IsDefined()); a != e {
			t.Errorf("%q.Compare(%q)==%v but expected %v", testCase.str, anUndef, a, e)
		}
		withAny, err := parsed.Parse(AnyAsString)
		if err != nil {
			t.Error(err)
		}
		if a, e := withAny.Compare(), c(parsed.IsAny(), parsed.IsAny()); a != e {
			t.Errorf("%q.Compare(%q)==%v but expected %v", testCase.str, anAny, a, e)
		}
		withSelf, err := parsed.Parse(testCase.str)
		if err != nil {
			t.Error(err)
		}
		if a, e := withSelf.Compare(), c(true, true); a != e {
			t.Errorf("%q.Compare(itself)==%v but expected %v", testCase.str, a, e)
		}
		emax := []string{testCase.str}
		if parsed.IsAny() {
			emax = append(emax, UndefinedAsString)
		}
		un := withUndef.Union()
		if p2, _ := un.Parse(emax[0]); p2.Compare().IsEqual() {
		} else if p2, _ := un.Parse(emax[len(emax)-1]); p2.Compare().IsEqual() {
		} else {
			t.Errorf("%q.Union(%q)==%#+v but expected one of %#+v", testCase.str, anUndef, un, emax)
		}
		emax = []string{testCase.str}
		if !parsed.IsDefined() {
			emax = append(emax, AnyAsString)
		}
		un = withAny.Union()
		if p2, _ := un.Parse(emax[0]); p2.Compare().IsEqual() {
		} else if p2, _ := un.Parse(emax[len(emax)-1]); p2.Compare().IsEqual() {
		} else {
			t.Errorf("%q.Union(%q)==%#+v but expected itself", testCase.str, anAny, un)
		}
		un = withSelf.Union()
		if p2, _ := un.Parse(testCase.str); !p2.Compare().IsEqual() {
			t.Errorf("%q.Union(itself)==%#+v but expected itself", testCase.str, un)
		}
	}
	for _, testCase := range []struct {
		str1, str2 string
		max        string
		comp       Comparison
	}{
		{"b=2", "a=10", "a=10,b=2", c(false, false)},
		{"a=10", "b=2", "a=10,b=2", c(false, false)},
		{"b=2", "a=10,b=2", "a=10,b=2", c(true, false)},
		{"b=3", "a=10,b=2", "a=10,b=3", c(false, false)},
		{"b=10,a=3", "a=2", "a=3,b=10", c(false, true)},
		{"b=10,a=3", "a=5", "a=5,b=10", c(false, false)},
		{"b=10,a=3", "a=3,b=10", "a=3,b=10", c(true, true)},
	} {
		p1, _ := ParseRVector(testCase.str1)
		p2, _ := p1.Parse(testCase.str2)
		acomp := p2.Compare()
		if acomp != testCase.comp {
			t.Errorf("%q.Compare(%q)=%v not %v", testCase.str1, testCase.str2, acomp, testCase.comp)
		}
		amax := p2.Union()
		if a, e := amax.String(), testCase.max; a != e {
			t.Errorf("%q.Union(%q)=%q not %q", testCase.str1, testCase.str2, a, e)
		}
	}
}

func c(le, ge bool) Comparison {
	return Comparison{LE: le, GE: ge}
}
