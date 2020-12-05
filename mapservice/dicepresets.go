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
////////////////////////////////////////////////////////////////////////////////////////
//                                                                                    //
//                                Dice Presets                                        //
//                                                                                    //
// Persistent storage and management of personal die-roll presets for each user.      //
//                                                                                    //
////////////////////////////////////////////////////////////////////////////////////////

package mapservice

import (
	"database/sql"
	"fmt"
)

////////////////////////////////////////////////////////////////////////////////// 
//  ____  _          ____                     _   
// |  _ \(_) ___ ___|  _ \ _ __ ___  ___  ___| |_ 
// | | | | |/ __/ _ \ |_) | '__/ _ \/ __|/ _ \ __|
// | |_| | | (_|  __/  __/| | |  __/\__ \  __/ |_ 
// |____/|_|\___\___|_|   |_|  \___||___/\___|\__|
//                                                
//
// A DicePreset represents a single die-roll that is saved for future
// use.
type DicePreset struct {
	Name	    string	// unique name; everything up to '|' does not display to the user but may be used for sorting list on the client.
	Description string  // user-defined description of what this die roll is for.
	RollSpec    string  // die-roll specification (see GMA documentation for syntax details).
}

//
// NewDicePreset creates a new DicePreset object.
//
func NewDicePreset(name, desc, roll string) *DicePreset {
	dp := new(DicePreset)
	dp.Name = name
	dp.Description = desc
	dp.RollSpec = roll

	return dp
}

//
// Database Schema
//  ________________       ________________
// | users          |     | dicepresets    |
// |----------------|     |----------------|
// | userid     PAi |---->| userid       i |
// | username     s |     | presetid   PAi |
// | password     s |     | name         s |
// |________________|     | description  s |
//                        | rollspec     s |
//                        |________________|
// 
// P=primary key
// A=auto-increment
// i=integer
// s=string

//
// LoadDicePresets loads the set of presets from storage
// and returns a map from user name to list of presets
//
// Returns a map of username:presets representing all
// stored preset settings.
//
func LoadDicePresets(db *sql.DB) (map[string][]DicePreset, error) {
	all_presets := make(map[string][]DicePreset)

	preset, err := db.Query(`
		select username, name, description, rollspec 
			from users, dicepresets 
			where users.userid=dicepresets.userid`)
	if err != nil {
		return nil, err
	}
	defer preset.Close()
	for preset.Next() {
		var (
			user string
			name string
			desc string
			spec string
		)
		if err = preset.Scan(&user, &name, &desc, &spec); err != nil {
			return nil, fmt.Errorf("unable to read die presets: %v", err)
		}
		plist, existing := all_presets[user]
		if !existing {
			plist = make([]DicePreset,0)
		}
		plist = append(plist, DicePreset{Name: name, Description: desc, RollSpec: spec})
		all_presets[user] = plist
	}

	return all_presets, nil
}

//
// SaveDicePresets saves an entire map of all presets as returned
// by LoadDicePresets to a persistent storage area.
//
func SaveDicePresets(db *sql.DB, collection map[string][]DicePreset) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("Unable to initiate dice preset save: %v", err)
	}

	_, err = tx.Exec(`delete from dicepresets;`)
	if err != nil { goto bail_out }

	for user, presets := range collection {
		var user_id int64

		result, err := tx.Query(`select userid from users where username = ?`, user)
		if err != nil { goto bail_out }
		if !result.Next() {
			// the user doesn't exist yet
			iresult, err := tx.Exec(`insert into users (username) values (?)`, user)
			if err != nil { goto bail_out }
			user_id, err = iresult.LastInsertId()
			if err != nil {goto bail_out}
		} else {
			// existing user, get their id
			if err = result.Scan(&user_id); err != nil { goto bail_out }
		}
		result.Close()

		for _, preset := range presets {
			if _, err = tx.Exec(`
				insert into dicepresets
					(userid, name, description, rollspec)
				values
					(?, ?, ?, ?)
			`, user_id, preset.Name, preset.Description, preset.RollSpec); err != nil { goto bail_out }
		}
	}

	if err = tx.Commit(); err != nil { goto bail_out }

	return nil

bail_out:
	if rberr := tx.Rollback(); rberr != nil {
		return fmt.Errorf("Error writing to dice preset database (%v); further, I failed to rollback that operation (%v)!", err, rberr)
	}
	return fmt.Errorf("Error writing to dice preset database (%v)", err)
}

//
// UpdateDicePresets changes the set of presets for a given user
// in the persistent storage to record that change without
// saving the entire collection.
//
func UpdateDicePresets(db *sql.DB, user string, presets []DicePreset) error {
	var user_id int64

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("Unable to initiate dice preset update: %v", err)
	}

	result, err := tx.Query(`select userid from users where username = ?`, user)
	if err != nil { goto bail_out }
	if !result.Next() {
		iresult, err := tx.Exec(`insert into users (username) values (?)`, user)
		if err != nil { goto bail_out }
		user_id, err = iresult.LastInsertId()
		if err != nil { goto bail_out }
	} else {
		if err = result.Scan(&user_id); err != nil { goto bail_out }
	}
	result.Close()

	_, err = tx.Exec(`delete from dicepresets where userid = ?`, user_id)
	if err != nil { goto bail_out }

	for _, preset := range presets {
		if _, err = tx.Exec(`
			insert into dicepresets
				(userid, name, description, rollspec)
			values
				(?, ?, ?, ?)
		`, user_id, preset.Name, preset.Description, preset.RollSpec); err != nil { goto bail_out }
	}

	if err = tx.Commit(); err != nil { goto bail_out }

	return nil

bail_out:
	if rberr := tx.Rollback(); rberr != nil {
		return fmt.Errorf("Error writing to dice preset database (%v) for user %s; further, I failed to rollback that operation (%v)!", err, user, rberr)
	}
	return fmt.Errorf("Error writing to dice preset database (%v) for user %s", err, user)
}

func NewDicePresetListFromString(srep string) ([]DicePreset, error) {
	var plist []DicePreset
	var err error

	slist, err := ParseTclList(srep)
	if err != nil { return nil, err }
	for _, p := range slist {
		thisPreset, err := NewDicePresetFromString(p)
		if err != nil { return nil, err }
		plist = append(plist, thisPreset)
	}
	return plist, nil
}

func NewDicePresetFromString(srep string) (DicePreset, error) {
	flist, err := ParseTclList(srep)
	if err != nil { return DicePreset{"","",""}, err }
	if len(flist) != 3 {
		return DicePreset{"","",""}, fmt.Errorf("Wrong number of values in die roll preset \"%s\"", srep)
	}
	return DicePreset{
		Name: flist[0],
		Description: flist[1],
		RollSpec: flist[2],
	}, nil
}

func DicePresetListToString(presets []DicePreset) (string, error) {
	var plist []string
	for _, p := range presets {
		s, err := DicePresetToString(p)
		if err != nil { return "", err }
		plist = append(plist, s)
	}
	return ToTclString(plist)
}

func DicePresetToString(preset DicePreset) (string, error) {
	return ToTclString([]string{preset.Name, preset.Description, preset.RollSpec})
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
