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
// Unit tests for the dice presets
//

package mapservice

import (
	"database/sql"
	_ "github.com/mattn/go-sqlite3"
	"github.com/google/go-cmp/cmp"
	"testing"
)

//
// mock database for our testcases
//
func TestLoadDicePresets(t *testing.T) {
	db, err := sql.Open("sqlite3", "file:__testL.db")
	if err != nil { t.Fatalf("error opening database: %v", err) }

	_, err = db.Exec(`drop table if exists users;
	drop table if exists dicepresets;
	create table users (
		userid integer primary key,
		username text not null
	);
	create table dicepresets (
		userid integer not null,
		presetid integer primary key,
		name text not null,
		description text not null,
		rollspec text not null,
		foreign key (userid) references users (userid) on delete cascade);`)
	if err != nil  { t.Fatalf("error initializing database: %v", err) }

	p, err := LoadDicePresets(db)
	if err != nil  { t.Errorf("error querying empty db: %v", err) }
	if len(p) != 0 { t.Errorf("empty db didn't yield an empty preset list") }
	if p == nil    { t.Errorf("empty db produced nil preset list") }

	_, err = db.Exec(`
		insert into users (username) values ("steve");
		insert into dicepresets (userid, name, description, rollspec)
			values ((select userid from users where username="steve"),
				"test set", "test", "1d20");`)
	if err != nil  { t.Fatalf("error initializing small database: %v", err) }

	p, err = LoadDicePresets(db)
	if err != nil  { t.Errorf("error querying small db: %v", err) }
	if len(p) != 1 { t.Errorf("small db should have 1 result, got %d", len(p)) }

	expected := map[string][]DicePreset{
		"steve": []DicePreset{
			{Name: "test set", Description: "test", RollSpec: "1d20"},
		},
	}
	if !cmp.Equal(p, expected) {
		t.Errorf("small db returned different data than expected: %s", cmp.Diff(expected, p))
	}

	_, err = db.Exec(`
		insert into users (username)
			values 
				("cassandra"),
				("kenny"),
				("jon");
		insert into dicepresets (userid, name, description, rollspec)
			values 
				((select userid from users where username="steve"), "test set 2", "", "1d20"),
				((select userid from users where username="steve"), "001|a", "test", "16d20+2|dc12"),
				((select userid from users where username="steve"), "002|b", "****", "6d6+2d6 fire"),
				((select userid from users where username="cassandra"), "abc", "", "d%"),
				((select userid from users where username="cassandra"), "def", "", "42%"),
				((select userid from users where username="jon"), "xx", "some dice", "256 d 4"),
				((select userid from users where username="jon"), "yy", "", "3d10");
	`)
	if err != nil  { t.Fatalf("error initializing small database: %v", err) }

	p, err = LoadDicePresets(db)
	if err != nil  { t.Errorf("error querying normal db: %v", err) }

	expected = map[string][]DicePreset{
		"steve": []DicePreset{
			{Name: "test set",   Description: "test", RollSpec: "1d20"},
			{Name: "test set 2", Description: "",     RollSpec: "1d20"},
			{Name: "001|a",      Description: "test", RollSpec: "16d20+2|dc12"},
			{Name: "002|b",      Description: "****", RollSpec: "6d6+2d6 fire"},
		},
		"cassandra": []DicePreset{
			{Name: "abc", RollSpec: "d%"},
			{Name: "def", RollSpec: "42%"},
		},
		"jon": []DicePreset{
			{Name: "xx", Description: "some dice", RollSpec: "256 d 4"},
			{Name: "yy", RollSpec: "3d10"},
		},
	}
	if !cmp.Equal(p, expected) {
		t.Errorf("normal db returned different data than expected: %s", cmp.Diff(expected, p))
	}

	db.Close()
}

func TestSaveDicePresets(t *testing.T) {
	db, err := sql.Open("sqlite3", "file:__testS.db")
	if err != nil { t.Fatalf("error opening database: %v", err) }

	_, err = db.Exec(`drop table if exists users;
	drop table if exists dicepresets;
	create table users (
		userid integer primary key,
		username text not null
	);
	create table dicepresets (
		userid integer not null,
		presetid integer primary key,
		name text not null,
		description text not null,
		rollspec text not null,
		foreign key (userid) references users (userid) on delete cascade);`)
	if err != nil  { t.Fatalf("error initializing database: %v", err) }

	p := map[string][]DicePreset{}
	err = SaveDicePresets(db, p)
	if err != nil   { t.Errorf("error writing empty db: %v", err) }

	p2, err := LoadDicePresets(db)
	if err != nil   { t.Errorf("error querying empty db: %v", err) }
	if len(p2) != 0 { t.Errorf("empty db didn't yield an empty preset list") }
	if p2 == nil    { t.Errorf("empty db produced nil preset list") }

	p = map[string][]DicePreset{
		"alice": []DicePreset{
			{Name: "aaa|bbb", Description: "have some fire, scarecrow!", RollSpec: "6d6 fire"},
			{Name: "aab|bbb", RollSpec: "4d8+12"},
		},
		"bob": []DicePreset{
			{Name: "00", RollSpec: "16d1024"},
			{Name: "01", RollSpec: "15%"},
		},
	}

	err = SaveDicePresets(db, p)
	if err != nil   { t.Errorf("error writing db: %v", err) }

	p2, err = LoadDicePresets(db)
	if err != nil  { t.Errorf("error querying small db: %v", err) }
	if !cmp.Equal(p, p2) {
		t.Errorf("small db returned different data than expected: %s", cmp.Diff(p, p2))
	}

	p = map[string][]DicePreset{
		"alice": []DicePreset{
			{Name: "aaa|bbb", Description: "have some fire, scarecrow!", RollSpec: "6d6 fire"},
			{Name: "aab|bbb", RollSpec: "4d8+12"},
		},
		"charlie": []DicePreset{
			{Name: "a0", RollSpec: "26d1024"},
			{Name: "a1", RollSpec: "25%"},
		},
	}

	err = SaveDicePresets(db, p)
	if err != nil   { t.Errorf("error writing db 2: %v", err) }

	p2, err = LoadDicePresets(db)
	if err != nil  { t.Errorf("error querying small db 2: %v", err) }
	if !cmp.Equal(p, p2) {
		t.Errorf("small db 2 returned different data than expected: %s", cmp.Diff(p, p2))
	}

	db.Close()
}

func TestUpdateDicePresets(t *testing.T) {
	db, err := sql.Open("sqlite3", "file:__testU.db")
	if err != nil { t.Fatalf("error opening database: %v", err) }

	_, err = db.Exec(`drop table if exists users;
	drop table if exists dicepresets;
	create table users (
		userid integer primary key,
		username text not null
	);
	create table dicepresets (
		userid integer not null,
		presetid integer primary key,
		name text not null,
		description text not null,
		rollspec text not null,
		foreign key (userid) references users (userid) on delete cascade);`)
	if err != nil  { t.Fatalf("error initializing database: %v", err) }

	p := []DicePreset{}
	err = UpdateDicePresets(db, "niluser", p)
	if err != nil   { t.Errorf("error writing empty db: %v", err) }

	p2, err := LoadDicePresets(db)
	if err != nil   { t.Errorf("error querying empty db: %v", err) }
	if len(p2) != 0 { t.Errorf("empty db didn't yield an empty preset list") }
	if p2 == nil    { t.Errorf("empty db produced nil preset list") }

	_, err = db.Exec(`insert into users (username)
		values ("alice"), ("bob");
		insert into dicepresets (userid, name, description, rollspec)
			values 
				((select userid from users where username="alice"), 
					"aaa|bbb", "have some fire, scarecrow!", "6d6 fire"),
				((select userid from users where username="alice"), 
					"aab|bbb", "", "4d8+12");`)
	if err != nil  { t.Fatalf("error filling database: %v", err) }
	err = UpdateDicePresets(db, "bob", []DicePreset{
		{Name: "00", RollSpec: "16d1024"},
		{Name: "01", RollSpec: "15%", Description: "xxxyyy"},
	})
	if err != nil  { t.Fatalf("error updating database: %v", err) }

	pe := map[string][]DicePreset{
		"alice": []DicePreset{
			{Name: "aaa|bbb", Description: "have some fire, scarecrow!", RollSpec: "6d6 fire"},
			{Name: "aab|bbb", RollSpec: "4d8+12"},
		},
		"bob": []DicePreset{
			{Name: "00", RollSpec: "16d1024"},
			{Name: "01", Description: "xxxyyy", RollSpec: "15%"},
		},
	}

	p2, err = LoadDicePresets(db)
	if err != nil  { t.Errorf("error querying small db: %v", err) }
	if !cmp.Equal(pe, p2) {
		t.Errorf("small db returned different data than expected: %s", cmp.Diff(pe, p2))
	}
	db.Close()
}

func TestDicePresetsNewListFromString(t *testing.T) {
	pl, err := NewDicePresetListFromString("{aa bb cc} {x {} {a b c}}")
	if err != nil { t.Fatalf("error %v", err) }
	if len(pl) != 2 { t.Fatalf("list length %d, 2 expected", len(pl)) }
	if pl[0].Name != "aa" { t.Errorf("pl 0 Name \"%s\"", pl[0].Name) }
	if pl[0].Description != "bb" { t.Errorf("pl 0 Description \"%s\"", pl[0].Description) }
	if pl[0].RollSpec != "cc" { t.Errorf("pl 0 RollSpec \"%s\"", pl[0].RollSpec) }
	if pl[1].Name != "x" { t.Errorf("pl 0 Name \"%s\"", pl[1].Name) }
	if pl[1].Description != "" { t.Errorf("pl 0 Description \"%s\"", pl[1].Description) }
	if pl[1].RollSpec != "a b c" { t.Errorf("pl 0 RollSpec \"%s\"", pl[1].RollSpec) }
}

func TestDicePresetsToString(t *testing.T) {
	s, err := DicePresetListToString([]DicePreset{
		{Name: "aa", Description: "", RollSpec: "d20+12"},
		{Name: "bb", Description: "test", RollSpec: "2d10-5+2d6 fire"},
		{Name: "xxx", Description: "test2", RollSpec: "15%"},
	})
	if err != nil { t.Fatalf("error %v", err) }
	if s != "{aa {} d20+12} {bb test {2d10-5+2d6 fire}} {xxx test2 15%}" {
		t.Errorf("string form was \"%s\"", s)
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
