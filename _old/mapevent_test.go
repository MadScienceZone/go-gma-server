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
// Unit tests for the map service
//

package mapservice

import "testing"

// Ensure that the supported version of the service protocol
// matches the one that the rest of the GMA suite believes to
// be the current version.
func TestMapEvent(t *testing.T) {
	type testcase struct {
		raw string
		etype string
		key string
		oid string
		cls string
		icls string
		err bool
	}
	tests := []testcase{
		{raw: "CLR",   etype: "CLR", err: true},
		{raw: "A B C", etype: "A",   err: true},
		{raw: "",      etype: "",    err: true},
		{raw: "LS",    etype: "LS",  key: "LS:abc", oid: "abc"},
		{raw: "// foo",etype: "//"},
		{raw: "AC foo",etype: "AC",  err: true},
		{raw: "AI foo bar",etype: "AI"},
		{raw: "AI: foo",etype: "AI:"},
		{raw: "AI. foo",etype: "AI."},
		{raw: "AI. foo bar",etype: "AI."},
		{raw: "AI? foo bar",etype: "AI?"},
		{raw: "AUTH foo",etype: "AUTH"},
		{raw: "D foo bar",etype: "D"},
		{raw: "DD foo",etype: "DD"},
		{raw: "DD= foo",etype: "DD=", err: true},
		{raw: "DD: foo",etype: "DD:", err: true},
		{raw: "DD. foo bar",etype: "DD.", err: true},
		{raw: "DR",etype: "DR"},
		{raw: "L foo",etype: "L"},
		{raw: "M foo",etype: "M"},
		{raw: "MARCO",etype: "MARCO"},
		{raw: "NO",etype: "NO"},
		{raw: "NO+",etype: "NO+"},
		{raw: "OK foo",etype: "OK", err: true},
		{raw: "POLO",etype: "POLO"},
		{raw: "SYNC foo",etype: "SYNC"},
		{raw: "DSM foo x x",etype: "DSM"},
		{raw: "TO foo x x x",etype: "TO"},
		{raw: "CLR x", etype: "CLR", key: "CLR:x"},
		{raw: "CLR@ x", etype: "CLR@", key: "CLR@:x"},
		{raw: "M? x", etype: "M?", key: "M?:x"},
		{raw: "M@ x", etype: "M@", key: "M@:x"},
		{raw: "OA foo {xx 1 yy 2 zz 3}",etype: "OA",  key: "OA:foo:xx:yy:zz"},
		{raw: "OA foo {yy 1 xx 2 zz 3}",etype: "OA",  key: "OA:foo:xx:yy:zz"},
		{raw: "OA foo {yy 1 xx 2 zz 3}",etype: "OA",  key: "OA:foo:xx:yy:zz", oid: "foo", err: false},
		{raw: "OA foo {yy 1 xx 2 zz 3}",etype: "OA",  key: "OA:foo:xx:yy:zz", oid: "bar", err: false},
		{raw: "OA foo {yy 1 xx 2 zz 3} a",etype: "OA",  key: "OA:foo:xx:yy:zz", err: true},
		{raw: "OA foo {{y}y 1 xx 2 zz 3}",etype: "OA",  key: "OA:foo:xx:yy:zz", err: true},
		{raw: "OA foo",etype: "OA",  key: "OA:foo:xx:yy:zz", err: true},
		{raw: "OA foo {X 1 Y 2 Z 3}",etype: "OA",  key: "OA:foo:X:Y:Z", cls: "E"},
		{raw: "OA foo {ANCHOR 1}",etype: "OA",  key: "OA:foo:ANCHOR", cls: "E"},
		{raw: "OA foo {AOESHAPE 1}",etype: "OA",  key: "OA:foo:AOESHAPE", cls: "E"},
		{raw: "OA foo {ARCMODE 1}",etype: "OA",  key: "OA:foo:ARCMODE", cls: "E"},
		{raw: "OA foo {ARROW 1}",etype: "OA",  key: "OA:foo:ARROW", cls: "E"},
		{raw: "OA foo {DASH 1}",etype: "OA",  key: "OA:foo:DASH", cls: "E"},
		{raw: "OA foo {EXTENT 1}",etype: "OA",  key: "OA:foo:EXTENT", cls: "E"},
		{raw: "OA foo {FILL 1}",etype: "OA",  key: "OA:foo:FILL", cls: "E"},
		{raw: "OA foo {FONT 1}",etype: "OA",  key: "OA:foo:FONT", cls: "E"},
		{raw: "OA foo {LAYER 1}",etype: "OA",  key: "OA:foo:LAYER", cls: "E"},
		{raw: "OA foo {LEVEL 1}",etype: "OA",  key: "OA:foo:LEVEL", cls: "E"},
		{raw: "OA foo {LINE 1}",etype: "OA",  key: "OA:foo:LINE", cls: "E"},
		{raw: "OA foo {POINTS 1}",etype: "OA",  key: "OA:foo:POINTS", cls: "E"},
		{raw: "OA foo {SPLINE 1}",etype: "OA",  key: "OA:foo:SPLINE", cls: "E"},
		{raw: "OA foo {START 1}",etype: "OA",  key: "OA:foo:START", cls: "E"},
		{raw: "OA foo {WIDTH 1}",etype: "OA",  key: "OA:foo:WIDTH", cls: "E"},
		{raw: "OA foo {X 2}",etype: "OA",  key: "OA:foo:X", cls: "E"},
		{raw: "OA foo {Y 3}",etype: "OA",  key: "OA:foo:Y", cls: "E"},
		{raw: "OA foo {Z 1}",etype: "OA",  key: "OA:foo:Z", cls: "E"},
		{raw: "OA foo {AOE 1}",etype: "OA",  key: "OA:foo:AOE", cls: "M"},
		{raw: "OA foo {AREA 1}",etype: "OA",  key: "OA:foo:AREA", cls: "M"},
		{raw: "OA foo {COLOR 1}",etype: "OA",  key: "OA:foo:COLOR", cls: "M"},
		{raw: "OA foo {DIM 1}",etype: "OA",  key: "OA:foo:DIM", cls: "M"},
		{raw: "OA foo {ELEV 1}",etype: "OA",  key: "OA:foo:ELEV", cls: "M"},
		{raw: "OA foo {GX 1}",etype: "OA",  key: "OA:foo:GX", cls: "M"},
		{raw: "OA foo {GY 1}",etype: "OA",  key: "OA:foo:GY", cls: "M"},
		{raw: "OA foo {HEALTH 1}",etype: "OA",  key: "OA:foo:HEALTH", cls: "M"},
		{raw: "OA foo {KILLED 1}",etype: "OA",  key: "OA:foo:KILLED", cls: "M"},
		{raw: "OA foo {MOVEMODE 1}",etype: "OA",  key: "OA:foo:MOVEMODE", cls: "M"},
		{raw: "OA foo {NAME 1}",etype: "OA",  key: "OA:foo:NAME", cls: "M"},
		{raw: "OA foo {NOTE 1}",etype: "OA",  key: "OA:foo:NOTE", cls: "M"},
		{raw: "OA foo {REACH 1}",etype: "OA",  key: "OA:foo:REACH", cls: "M"},
		{raw: "OA foo {SIZE 1}",etype: "OA",  key: "OA:foo:SIZE", cls: ""},
		{raw: "OA foo {SKIN 1}",etype: "OA",  key: "OA:foo:SKIN", cls: "M"},
		{raw: "OA foo {STATUSLIST 1}",etype: "OA",  key: "OA:foo:STATUSLIST", cls: "M"},
		{raw: "OA foo {STATUSLIST 1}",etype: "OA",  key: "OA:foo:STATUSLIST", cls: "X", icls: "X"},
		{raw: "OA+ bar STATUSLIST {a b c}", etype: "OA+", key: "OA+:bar:STATUSLIST:a:b:c", cls: "X", icls: "X"},
		{raw: "OA+ bar STATUSLIST {c}",     etype: "OA+", key: "OA+:bar:STATUSLIST:c", cls: "X", icls: "X"},
		{raw: "OA+ bar STATUSLIST {}",      etype: "OA+", key: "OA+:bar:STATUSLIST:", cls: "X", icls: "X"},
		{raw: "OA- bar STATUSLIST {a b c}", etype: "OA-", key: "OA-:bar:STATUSLIST:a:b:c", cls: "X", icls: "X"},
		{raw: "OA- bar STATUSLIST {c}",     etype: "OA-", key: "OA-:bar:STATUSLIST:c", cls: "X", icls: "X"},
		{raw: "OA- bar STATUSLIST {}",      etype: "OA-", key: "OA-:bar:STATUSLIST:", cls: "M"},
		{raw: "PS id color name area size player x y reach", etype: "PS", key: "PS:id", cls: "P"},
		{raw: "PS id color name area size monster x y reach", etype: "PS", key: "PS:id", cls: "M"},
		{raw: "PS id color name area size gremlin x y reach", err: true},
		{raw: "PS id color name area size", err: true},
		{raw: "AV x y", etype: "AV", key: "AV"},
		{raw: "CO x", etype: "CO", key: "CO"},
		{raw: "CS x x", etype: "CS", key: "CS"},
		{raw: "I x i", etype: "I", key: "I"},
		{raw: "IL x", etype: "IL", key: "IL"},
		{raw: "TB x", etype: "TB", key: "TB"},
	}
	for _, tc := range tests {
		t.Run("type test", func(t *testing.T) {
			ev, err := NewMapEvent(tc.raw, tc.oid, tc.icls)
			if tc.err {
				if err == nil {
					t.Fatalf("Constructing map event from %s should have caused an error, but didn't.", tc.raw)
				}
			} else {
				if err != nil {
					t.Fatalf("Constructing map event from %s resulted in error %v", tc.raw, err)
				}
				if ev.EventType() != tc.etype {
					t.Errorf("Map event from %s is type %s, expected %s", tc.raw, ev.EventType(), tc.etype)
				}
				if ev.EventKey() != tc.key {
					t.Errorf("Map event from %s has key %s, expected %s", tc.raw, ev.EventKey(), tc.key)
				}
				if ev.EventClass() != tc.cls {
					t.Errorf("Map event from %s has class %s, expected %s", tc.raw, ev.EventClass(), tc.cls)
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
