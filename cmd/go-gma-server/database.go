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
// Database subsystem for the map server. This stores the persistent data the server
// needs to maintain between sessions. Note that the game state is now considered
// too ephemeral to pay the cost of constantly writing it to the database.
//

package main

import (
	"database/sql"
	"os"

	"github.com/MadScienceZone/go-gma/v5/mapper"
	_ "github.com/mattn/go-sqlite3"
)

func (a *Application) dbOpen() error {
	var err error

	if a.DatabaseName == "" {
		a.sqldb = nil
		return nil
	}

	if _, err = os.Stat(a.DatabaseName); os.IsNotExist(err) {
		// database doesn't exist yet; create a new one

		a.Logf("no existing sqlite3 database \"%s\" found--creating a new one", a.DatabaseName)
		a.sqldb, err = sql.Open("sqlite3", "file:"+a.DatabaseName)
		if err != nil {
			a.Logf("unable to create sqlite3 database %s: %v", a.DatabaseName, err)
			return err
		}

		_, err = a.sqldb.Exec(`
			create table users (
				userid integer primary key,
				username text not null
			);
			create table dicepresets (
				presetid    integer primary key,
				userid      integer not null,
				name        text    not null,
				description text    not null,
				rollspec    text    not null,
					foreign key (userid)
						references users (userid)
						on delete cascade
			);
			create table chats (
				msgid   integer primary key,
				rawdata text    not null
			);
			create table images (
				name	text	not null,
				zoom    real    not null,
				location text   not null,
				islocal integer(1) not null,
					primary key (name,zoom)
		);`)

		if err != nil {
			a.Logf("unable to create sqlite3 database %s contents: %v", a.DatabaseName, err)
			return err
		}
	} else {
		a.sqldb, err = sql.Open("sqlite3", "file:"+a.DatabaseName)
	}
	return err
}

func (a *Application) dbClose() error {
	if a.sqldb == nil {
		return nil
	}
	return a.sqldb.Close()
}

func (a *Application) StoreImageData(imageName string, img mapper.ImageInstance) error {
	result, err := a.sqldb.Exec(`REPLACE INTO images (name, zoom, location, islocal) VALUES (?, ?, ?, ?);`, imageName, img.Zoom, img.File, img.IsLocalFile)
	if err != nil {
		a.Logf("error storing image record \"%s\"@%v local=%v, ID=%v: %v", imageName, img.Zoom, img.IsLocalFile, img.File, err)
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		a.Debugf(DebugDB, "stored image record \"%s\"@%v local=%v, ID=%v, (unable to examine results: %v)", imageName, img.Zoom, img.IsLocalFile, img.File, err)
	} else {
		a.Debugf(DebugDB, "stored image record \"%s\"@%v local=%v, ID=%v, rows affected=%d", imageName, img.Zoom, img.IsLocalFile, img.File, affected)
	}
	return nil
}

func (a *Application) QueryImageData(img mapper.ImageDefinition) (mapper.ImageDefinition, error) {
	var resultSet mapper.ImageDefinition

	a.Debugf(DebugDB, "query of image \"%s\"", img.Name)
	rows, err := a.sqldb.Query(`SELECT zoom, location, islocal FROM images WHERE name=?`, img.Name)
	if err != nil {
		a.Logf("error retrieving image data for \"%s\": %v", img.Name, err)
		return resultSet, err
	}
	defer rows.Close()

	resultSet.Name = img.Name
	for rows.Next() {
		var instance mapper.ImageInstance
		var isLocal int

		if err := rows.Scan(&instance.Zoom, &instance.File, &isLocal); err != nil {
			a.Logf("error scanning row of image data: %v", err)
			return resultSet, err
		}
		if isLocal != 0 {
			instance.IsLocalFile = true
		}
		resultSet.Sizes = append(resultSet.Sizes, instance)
		a.Debugf(DebugDB, "result: \"%s\"@%v from \"%s\" (local=%v)", img.Name, instance.Zoom, instance.File, instance.IsLocalFile)
	}
	if err := rows.Err(); err != nil {
		a.Logf("error retrieving rows of image data: %v", err)
	}
	return resultSet, err
}

// @[00]@| GMA 4.2.2
// @[01]@|
// @[10]@| Copyright © 1992–2020 by Steven L. Willoughby
// @[11]@| (AKA MadScienceZone), Aloha, Oregon, USA. All Rights Reserved.
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
