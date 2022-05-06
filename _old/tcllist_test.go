// vi:set ai sm nu ts=4 sw=4 fileencoding=utf-8:
/*
########################################################################################
#  _______  _______  _______                ___       _______     _______              #
# (  ____ \(       )(  ___  )              /   )     / ___   )   / ___   )             #
# | (    \/| () () || (   ) |             / /) |     \/   )  |   \/   )  |             #
# | |      | || || || (___) |            / (_) (_        /   )       /   )             #
# | | ____ | |(_)| ||  ___  |           (____   _)     _/   /      _/   /              #
# | | \_  )| |   | || (   ) | Game           ) (      /   _/      /   _/               #
# | (___) || )   ( || )   ( | Master's       | |   _ (   (__/\ _ (   (__/\             #
# (_______)|/     \||/     \| Assistant      (_)  (_)\_______/(_)\_______/             #
#                                                                                      #
########################################################################################
*/

//
// Unit tests for the tcllist type
//

package mapservice

import "testing"

// TCL LIST FORMAT
//        In  a  nutshell,  a  Tcl  list (as a string representation) is a space-
//        delimited list of values. Any value which includes spaces  is  enclosed
//        in  curly braces.  An empty string (empty list) as an element in a list
//        is represented as “{}”.  (E.g., “1 {} 2” is a list of  three  elements,
//        the  middle of which is an empty string.) An entirely empty Tcl list is
//        represented as an empty string “”.
// 
//        A list value must have balanced braces. A balanced pair of braces  that
//        happen  to  be  inside a larger string value may be left as-is, since a
//        string that happens to contain spaces or braces is  only  distinguished
//        from a deeply-nested list value when you attempt to interpret it as one
//        or another in the code. Thus, the list
//               “a b {this {is a} string}”
//        has three elements: “a”, “b”, and “this {is a} string”.   Otherwise,  a
//        lone brace that's part of a string value should be escaped with a back‐
//        slash:
//               “a b {this \{ too}”
// 
//        Literal backslashes may be escaped with a backslash as well.
// 
//        While extra spaces are ignored when  parsing  lists  into  elements,  a
//        properly  formed  string representation of a list will have the miminum
//        number of spaces and braces needed to describe the list structure.
//
// 		More examples
//			a b c d		    [a, b, c, d]
//			a {b c} d	    [a, "b c", d]			) depending on how you interpret
//			a {b c} d	    [a, [b, c], d]			) the answer
//			a b {{c d} e f} [a, b, "{c d} e f"]
//			a b {{c d} e f} [a, b, ["c d", e, f]]
//			a b {{c d} e f} [a, b, [[c, d], e, f]]

import (
	"reflect"
)

func TestTclList_Str2list(t *testing.T) {
	type testcase struct {
		tcl  string
		list []string
		is_error bool
	}

	tests := []testcase{
		{tcl: "a b c d",              list: []string{"a", "b", "c", "d"},        is_error: false},
		{tcl: "a  b c d",             list: []string{"a", "b", "c", "d"},        is_error: false},
		{tcl: "   a  b  c  d    ",    list: []string{"a", "b", "c", "d"},        is_error: false},
		{tcl: "a {b  c} d",           list: []string{"a", "b  c", "d"},          is_error: false},
		{tcl: "a {b  c}x d",          list: []string{"a", "b  c", "d"},          is_error: true},
		{tcl: "a {b c}{def} x d",     list: []string{"a", "b  c", "d"},          is_error: true},
		{tcl: "a b {{c d} e f}",      list: []string{"a", "b", "{c d} e f"},     is_error: false},
		{tcl: "a b {c d} e f}",       list: []string{"a", "b", "{c d} e f"},     is_error: true},
		{tcl: "a b{cd d} e f}",       list: []string{"a", "b", "{c d} e f"},     is_error: true},
		{tcl: "a b{cd d} e f",        list: []string{"a", "b{cd", "d}", "e","f"},is_error: false},
		{tcl: "a b{cd d}e e f",       list: []string{"a", "b{cd","d}e","e","f"}, is_error: false},
		{tcl: "a b{cd d}}e e f",      list: []string{"a", "b{cd","d}e","e","f"}, is_error: true},
		{tcl: "a b{cd d}{e e f",      list: []string{"a", "b{cd","d}e","e","f"}, is_error: true},
		{tcl: "               ",      list: []string{},                          is_error: false},
		{tcl: "",                     list: []string{},                          is_error: false},
		{tcl: "1 2 \"\" 5",           list: []string{"1", "2", "", "5"},         is_error: false},
		{tcl: "a \"b  c\" d",         list: []string{"a", "b  c", "d"},          is_error: false},
		{tcl: "a \"b  c\"x d",        list: []string{"a", "b  c", "d"},          is_error: true},
		{tcl: "a \"b c\"\"def\" x d", list: []string{"a", "b  c", "d"},          is_error: true},
		{tcl: "a b \"{c d} e f\"",    list: []string{"a", "b", "{c d} e f"},     is_error: false},
		{tcl: "a b \"c d\" e f}",     list: []string{"a", "b", "{c d} e f"},     is_error: true},
		{tcl: "a b\"cd d\" e f}",     list: []string{"a", "b", "{c d} e f"},     is_error: true},
		{tcl: "a b\"cd d\" e f",      list: []string{"a","b\"cd","d\"", "e","f"},is_error: false},
		{tcl: "a b\"cd d\"e e f",     list: []string{"a","b\"cd","d\"e","e","f"},is_error: false},
		{tcl: "1 2 {} 5",             list: []string{"1", "2", "", "5"},         is_error: false},
		{tcl: "spam eggs",            list: []string{"spam", "eggs"},            is_error: false},
		{tcl: "penguin {spam spam}",  list: []string{"penguin", "spam spam"},    is_error: false},
		{tcl: "penguin \\{spam spam}",list: []string{"penguin","{spam", "spam}"},is_error: true},
		{tcl: "penguin \\{spam spam\\}",list: []string{"penguin","{spam","spam}"},is_error: false},
		{tcl: "aa \\{\\\"bb\\}cc dd", list: []string{"aa", "{\"bb}cc", "dd"},    is_error: false},
		{tcl: "\\#aa bb dd",          list: []string{"#aa", "bb", "dd"},         is_error: false},
		{tcl: "\\#aa bb dd\\",        list: []string{"#aa", "bb", "dd"},         is_error: true},
		{tcl: "a b {this {is a} string}",list: []string{"a", "b", "this {is a} string"},is_error: false},
		{tcl: "a b {this \\{ too}",   list: []string{"a", "b", "this { too"},    is_error: false},
		{tcl: "a b this\\ \\{\\ too", list: []string{"a", "b", "this { too"},    is_error: false},
		{tcl: "^\\$\\[.*\\]",         list: []string{"^\\$\\[.*\\]"},            is_error: false},
	}

	for _, test := range tests {
		t.Run("parse tests", func(t *testing.T) {
			l, err := ParseTclList(test.tcl)
			if test.is_error {
				if err == nil {
					t.Fatalf("TCL \"%s\" was supposed to return an error but didn't.", test.tcl)
				}
			} else {
				if err != nil {
					t.Fatalf("TCL \"%s\" caused error \"%v\"", test.tcl, err)
				}
				if !reflect.DeepEqual(l, test.list) {
					t.Fatalf("TCL \"%s\" -> %v, expected %v",
						test.tcl, l, test.list)
				}
			}
		})
	}
}

func TestTclList_List2str(t *testing.T) {
	type testcase struct {
		tcl  string
		list []string
		is_error bool
	}

	tests := []testcase{
		{tcl: "a b c d",              list: []string{"a", "b", "c", "d"},        is_error: false},
		{tcl: "a {b  c} d",           list: []string{"a", "b  c", "d"},          is_error: false},
		{tcl: "a b {{c d} e f}",      list: []string{"a", "b", "{c d} e f"},     is_error: false},
        {tcl: "a b\\{cd d\\} e f",    list: []string{"a", "b{cd", "d}", "e","f"},is_error: false},
        {tcl: "a b\\{cd d\\}e e f",   list: []string{"a", "b{cd","d}e","e","f"}, is_error: false},
		{tcl: "",                     list: []string{},                          is_error: false},
		{tcl: "{}",                   list: []string{""},                        is_error: false},
		{tcl: "1 2 {} 5",             list: []string{"1", "2", "", "5"},         is_error: false},
		{tcl: "spam eggs",            list: []string{"spam", "eggs"},            is_error: false},
		{tcl: "penguin {spam spam}",  list: []string{"penguin", "spam spam"},    is_error: false},
		{tcl: "penguin \\{spam spam\\}",list: []string{"penguin","{spam","spam}"},is_error: false},
		{tcl: "aa {{\"bb}cc} dd",     list: []string{"aa", "{\"bb}cc", "dd"},    is_error: false},
		{tcl: "{#aa} bb dd",          list: []string{"#aa", "bb", "dd"},         is_error: false},
		{tcl: "a b {this {is a} string}",list: []string{"a", "b", "this {is a} string"},is_error: false},
		{tcl: "a b this\\ \\{\\ too",   list: []string{"a", "b", "this { too"},    is_error: false},
	}

	for _, test := range tests {
		t.Run("emit tests", func(t *testing.T) {
			s, err := ToTclString(test.list)
			if test.is_error {
				if err == nil {
					t.Fatalf("List %v was supposed to return an error but didn't.", test.list)
				}
			} else {
				if err != nil {
					t.Fatalf("list %v caused error \"%v\"", test.list, err)
				}
				if s != test.tcl {
					t.Errorf("List %v -> \"%s\", expected \"%s\"",
						test.list, s, test.tcl)
				}
				l, err := ParseTclList(test.tcl)
				if err != nil {
					t.Fatalf("List %v -> TCL \"%s\" -> List caused error \"%v\"", test.list, s, err)
				}
				if !reflect.DeepEqual(l, test.list) {
					t.Errorf("List %v -> TCL \"%s\" -> %v",
						test.list, s, l)
				}
			}
		})
	}
}

// @[00]@| GMA 4.2.2
// @[01]@|
// @[10]@| Copyright © 1992–2020 by Steven L. Willoughby
// @[11]@| (AKA Software Alchemy), Aloha, Oregon, USA. All Rights Reserved.
// @[12]@| Distributed under the terms and conditions of the BSD-3-Clause
// @[13]@| License as described in the accompanying LICENSE file distributed
// @[14]@| with GMA.
// @[15]@|
// @[20]@| Redistribution and use in source and binary forms, with or without
// @[21]@| modification, are permitted provided that the following conditions
// @[22]@| are met:
// @[23]@| 1. Redistributions of source code must retain the above copyright
// @[24]@|    notice, this list of conditions and the following disclaimer.
// @[25]@| 2. Redistributions in binary form must reproduce the above copy-
// @[26]@|    right notice, this list of conditions and the following dis-
// @[27]@|    claimer in the documentation and/or other materials provided
// @[28]@|    with the distribution.
// @[29]@| 3. Neither the name of the copyright holder nor the names of its
// @[30]@|    contributors may be used to endorse or promote products derived
// @[31]@|    from this software without specific prior written permission.
// @[32]@|
// @[33]@| THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND
// @[34]@| CONTRIBUTORS “AS IS” AND ANY EXPRESS OR IMPLIED WARRANTIES,
// @[35]@| INCLUDING, BUT NOT LIMITED TO, THE IMPLIED WARRANTIES OF
// @[36]@| MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
// @[37]@| DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS
// @[38]@| BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY,
// @[39]@| OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO,
// @[40]@| PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR
// @[41]@| PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
// @[42]@| THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR
// @[43]@| TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF
// @[44]@| THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF
// @[45]@| SUCH DAMAGE.
// @[46]@|
// @[50]@| This software is not intended for any use or application in which
// @[51]@| the safety of lives or property would be at risk due to failure or
// @[52]@| defect of the software.
//
