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
	anUndef, _ := ParseResourceVersion(UndefinedAsString)
	anAny, _ := ParseResourceVersion(AnyAsString)
	for _, testCase := range []struct {
		str      string
		parseErr bool
		isUndef  bool
		isAny    bool
	}{
		{"xyz", true, false, false},
		{UndefinedAsString, false, true, false},
		{AnyAsString, false, false, true},
		{"1", false, false, false},
		{"10", false, false, false},
	} {
		parsed, err := ParseResourceVersion(testCase.str)
		if (err != nil) != testCase.parseErr {
			t.Errorf("Parse of %q produced error %#+v", testCase.str, err)
		}
		if err != nil {
			continue
		}
		if a, e := parsed.IsUndefined(), testCase.isUndef; a != e {
			t.Errorf("%q.IsUndefined()==%v but expected %v", testCase.str, a, e)
		}
		if a, e := parsed.IsAny(), testCase.isAny; a != e {
			t.Errorf("%q.IsAny()==%v but expected %v", testCase.str, a, e)
		}
		if a, e := parsed.String(), testCase.str; a != e {
			t.Errorf("%q.String()==%q", e, a)
		}
		efile := testCase.str
		if efile == UndefinedAsString {
			efile = "undefined"
		}
		if a := parsed.StringForFilename(); a != efile {
			t.Errorf("%q.String()==%q", testCase.str, a)
		}
		if a, e := parsed.Compare(anUndef), c(parsed.IsUndefined(), parsed.IsUndefined()); a != e {
			t.Errorf("%q.Compare(%q)==%v but expected %v", testCase.str, anUndef, a, e)
		}
		if a, e := parsed.Compare(anAny), c(parsed.IsAny(), parsed.IsAny()); a != e {
			t.Errorf("%q.Compare(%q)==%v but expected %v", testCase.str, anAny, a, e)
		}
		if a, e := parsed.Compare(parsed), c(true, true); a != e {
			t.Errorf("%q.Compare(itself)==%v but expected %v", testCase.str, a, e)
		}
		emax := []ResourceVersion{parsed}
		if parsed.IsAny() {
			emax = append(emax, anUndef)
		}
		if m := parsed.Union(anUndef); !(m.Compare(emax[0]).IsEqual() || m.Compare(emax[len(emax)-1]).IsEqual()) {
			t.Errorf("%q.Union(%q)==%#+v but expected one of %#+v", testCase.str, anUndef, m, emax)
		}
		emax = []ResourceVersion{parsed}
		if parsed.IsUndefined() {
			emax = append(emax, anAny)
		}
		if m := parsed.Union(anAny); !(m.Compare(emax[0]).IsEqual() || m.Compare(emax[len(emax)-1]).IsEqual()) {
			t.Errorf("%q.Union(%q)==%#+v but expected itself", testCase.str, anAny, m)
		}
		if m := parsed.Union(parsed); !m.Compare(parsed).IsEqual() {
			t.Errorf("%q.Union(itself)==%#+v but expected itself", testCase.str, m)
		}
	}
	for _, testCase := range []struct {
		str1, str2 string
		max        string
		comp       Comparison
	}{
		{"10", "2", "10", c(false, true)},
		{"2", "10", "10", c(true, false)},
	} {
		p1, _ := ParseResourceVersion(testCase.str1)
		p2, _ := ParseResourceVersion(testCase.str2)
		acomp := p1.Compare(p2)
		if acomp != testCase.comp {
			t.Errorf("%q.Compare(%q)=%v not %v", testCase.str1, testCase.str2, acomp, testCase.comp)
		}
		amax := p1.Union(p2)
		if a, e := amax.String(), testCase.max; a != e {
			t.Errorf("%q.Union(%q)=%q not %q", testCase.str1, testCase.str2, a, e)
		}
	}
}

func c(le, ge bool) Comparison {
	return Comparison{LE: le, GE: ge}
}
