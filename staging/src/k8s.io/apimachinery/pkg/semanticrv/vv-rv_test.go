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
		str          string
		parseErr     bool
		isUndef      bool
		isAny        bool
		canonicalStr string
	}{
		{"xyz", true, false, false, ""},
		{UndefinedAsString, false, true, false, ""},
		{AnyAsString, false, false, true, ""},
		{"42", false, false, false, ""},
		{"a=2", false, false, false, ""},
		{"b=10", false, false, false, ""},
		{"a=2,b=10", false, false, false, ""},
		{"b=10,a=2", false, false, false, "a=2,b=10"},
		{"b=(y=10,x=20),c=30,a=2", false, false, false, "a=2,b=(x=20,y=10),c=30"},
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
		m, err := parsed.Union(anUndef)
		if err != nil {
			t.Errorf("%q.Union(%q) threw %#+v", testCase.str, anUndef, err)
		} else if !(m.Compare(emax[0]).IsEqual() || m.Compare(emax[len(emax)-1]).IsEqual()) {
			t.Errorf("%q.Union(%q)==%#+v but expected one of %#+v", testCase.str, anUndef, m, emax)
		}
		emax = []ResourceVersion{parsed}
		if parsed.IsUndefined() {
			emax = append(emax, anAny)
		}
		m, err = parsed.Union(anAny)
		if err != nil {
			t.Errorf("%q.Union(%q) threw %#+v", testCase.str, anAny, err)
		} else if !(m.Compare(emax[0]).IsEqual() || m.Compare(emax[len(emax)-1]).IsEqual()) {
			t.Errorf("%q.Union(%q)==%#+v but expected itself", testCase.str, anAny, m)
		}
		m, err = parsed.Union(parsed)
		if err != nil {
			t.Errorf("%q.Union(itself) threw %#+v", testCase.str, err)
		} else if !m.Compare(parsed).IsEqual() {
			t.Errorf("%q.Union(itself)==%#+v but expected itself", testCase.str, m)
		}
	}
	for _, testCase := range []struct {
		str1, str2 string
		union      string
		unionErr   bool
		comp       Comparison
	}{
		{"b=2", "a=10", "a=10,b=2", false, c(false, false)},
		{"a=10", "b=2", "a=10,b=2", false, c(false, false)},
		{"10", "a=20", "a=20", false, c(true, false)},
		{"b=20", "10", "b=20", false, c(false, true)},
		{"b=20,a=15", "10", "a=15,b=20", false, c(false, true)},
		{"30", "a=20", "30", false, c(false, true)},
		{"30", "a=20,b=25", "30", false, c(false, true)},
		{"b=20", "30", "30", false, c(true, false)},
		{"b=2", "a=10,b=2", "a=10,b=2", false, c(true, false)},
		{"b=3", "a=10,b=2", "a=10,b=3", false, c(false, false)},
		{"b=10,a=3", "a=2", "a=3,b=10", false, c(false, true)},
		{"b=10,a=3", "a=5", "a=5,b=10", false, c(false, false)},
		{"b=10,a=3", "a=3,b=10", "a=3,b=10", false, c(true, true)},
		{"c=30,a=20", "10", "a=20,c=30", false, c(false, true)},
		{"c=30,a=20", "20", "a=20,c=30", false, c(false, true)},
		{"c=30,a=20", "40", "40", false, c(true, false)},
		{"c=30,a=20", "30", "30", false, c(true, false)},
		{"c=30,a=20", "25", "xxx", true, c(false, false)},
		{"a=10,b=(x=30,y=40)", "a=(w=15,z=35),b=20", "a=(w=15,z=35),b=(x=30,y=40)", false, c(false, false)},
	} {
		p1, _ := ParseResourceVersion(testCase.str1)
		p2, _ := ParseResourceVersion(testCase.str2)
		acomp := p1.Compare(p2)
		if acomp != testCase.comp {
			t.Errorf("%q.Compare(%q) returned %v not %v", testCase.str1, testCase.str2, acomp, testCase.comp)
		}
		amax, err := p1.Union(p2)
		if (err != nil) != testCase.unionErr {
			t.Errorf("%q.Union(%q) returned (%#+v, %#+v)", testCase.str1, testCase.str2, amax, err)
		}
		if err == nil {
			if a, e := amax.String(), testCase.union; a != e {
				t.Errorf("%q.Union(%q) returned %q not %q", testCase.str1, testCase.str2, a, e)
			}
		}
	}
}

func c(le, ge bool) Comparison {
	return Comparison{LE: le, GE: ge}
}
