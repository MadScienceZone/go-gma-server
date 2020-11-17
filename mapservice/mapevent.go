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
////////////////////////////////////////////////////////////////////////////////////////
//                                                                                    //
//                                     MapEvent                                       //
//                                                                                    //
// The representation of a single event we received from a map client.                //
//                                                                                    //
////////////////////////////////////////////////////////////////////////////////////////

package mapservice

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"sync"
)

//
// MESSAGE_ID_EPOCH is a limiter on the initial run of message IDs.
// Messages sent over the chat channel (including die rolls) have unique ID
// numbers assigned to them. To provide a simple way to have a new server instance
// start off without (much) risk of overlapping IDs with previous instances, we
// start off with an ID equal to the current time in seconds on the system clock,
// and increment sequentially from there. To keep the numbers from starting off
// larger than necessary, we subtract the epoch value from that clock value.
// This number can be anthing greater than or equal to the epoch numbers you've
// used in the past. For brand-new servers, you can set this to the current
// time when installing the server, so message IDs will start off trivially small.
//
const MESSAGE_ID_EPOCH = 1593130546

////////////////////////////////////////////////////////////////////////////////// 
//  __  __             _____                 _   
// |  \/  | __ _ _ __ | ____|_   _____ _ __ | |_ 
// | |\/| |/ _` | '_ \|  _| \ \ / / _ \ '_ \| __|
// | |  | | (_| | |_) | |___ \ V /  __/ | | | |_ 
// |_|  |_|\__,_| .__/|_____| \_/ \___|_| |_|\__|
//              |_|                              
//
// A map_event represents a single message received by a client
// such as placing an element on the map or updating an object's
// parameters.
type MapEvent struct {
	MultiRawData []string
	Sequence     int
	Fields       []string
	Key          string
	Class        string
	ID           string
}

//
// methods required for MapEvent to implement the sort.Interface
// interface
//
type MapEventList []*MapEvent

func (e MapEventList) Len() int {
	return len(e)
}

func (e MapEventList) Less(i, j int) bool {
	return e[i].Sequence < e[j].Sequence
}

func (e MapEventList) Swap(i, j int) {
	e[i], e[j] = e[j], e[i]
}

// even_elements takes a slice of strings and returns only those elements
// with even-numbered indices (in a key-value list, this is all the keys)
func even_elements(in []string) []string {
	out := make([]string, 0, len(in)/2)
	for i, v := range in {
		if i%2 == 0 {
			out = append(out, v)
		}
	}
	return out
}

//
// Event validation
// Simple verification including number of fields for each command type, and
// that the type is known.
//
type map_event_parameters struct {
	MinParams	int
	MaxParams	int
}

var map_event_checklist map[string]map_event_parameters
var next_message_id int = 0
var message_id_lock sync.Mutex

func init() {
	next_message_id = int(time.Now().Unix() - MESSAGE_ID_EPOCH)

	map_event_checklist = map[string]map_event_parameters{
		"//":     {MinParams: 0, MaxParams: -1}, // //...
		"ACCEPT": {MinParams: 1, MaxParams:  1}, // ACCEPT list
		"AI":     {MinParams: 2, MaxParams:  2}, // AI name size
		"AI:":    {MinParams: 1, MaxParams:  1}, // AI: data
		"AI.":    {MinParams: 1, MaxParams:  2}, // AI. lines [cks]
		"AI?":    {MinParams: 2, MaxParams:  2}, // AI? name size
		"AI@":    {MinParams: 3, MaxParams:  3}, // AI@ name size id
		"AUTH":   {MinParams: 1, MaxParams:  3}, // AUTH response [user [client]]
		"AV":     {MinParams: 2, MaxParams:  2}, // AV x y
		"CC":     {MinParams: 0, MaxParams:  3}, // CC [user [target [id]]]
		"CLR":    {MinParams: 1, MaxParams:  1}, // CLR id
		"CLR@":   {MinParams: 1, MaxParams:  1}, // CLR@ id
		"CO":     {MinParams: 1, MaxParams:  1}, // CO state
		"CS":     {MinParams: 2, MaxParams:  2}, // CS abs rel
		"D":      {MinParams: 2, MaxParams:  2}, // D recipients dice
		"DD":     {MinParams: 1, MaxParams:  1}, // DD list
		"DD+":    {MinParams: 1, MaxParams:  1}, // DD+ list
		"DD/":    {MinParams: 1, MaxParams:  1}, // DD/ regex
		"DR":     {MinParams: 0, MaxParams:  0}, // DR
		"DSM":    {MinParams: 3, MaxParams:  3}, // DSM cond shape color
		"I":      {MinParams: 2, MaxParams:  2}, // I time id
		"IL":     {MinParams: 1, MaxParams:  1}, // IL slotlist
		"L":      {MinParams: 1, MaxParams:  1}, // L list
		"LS":     {MinParams: 0, MaxParams:  0}, // LS
		"LS:":    {MinParams: 0, MaxParams:  1}, // LS: [data]
		"LS.":    {MinParams: 1, MaxParams:  2}, // LS. count [cks]
		"M":      {MinParams: 1, MaxParams:  1}, // M list
		"M?":     {MinParams: 1, MaxParams:  1}, // M? id
		"M@":     {MinParams: 1, MaxParams:  1}, // M@ id
		"MARK":   {MinParams: 2, MaxParams:  2}, // MARK x y
		"MARCO":  {MinParams: 0, MaxParams:  0}, // MARCO
		"NO":     {MinParams: 0, MaxParams:  0}, // NO
		"NO+":    {MinParams: 0, MaxParams:  0}, // NO+
		"OA":     {MinParams: 2, MaxParams:  2}, // OA id kvlist
		"OA+":    {MinParams: 3, MaxParams:  3}, // OA+ id key vlist
		"OA-":    {MinParams: 3, MaxParams:  3}, // OA- id key vlist
		"POLO":   {MinParams: 0, MaxParams:  0}, // POLO
		"PS":     {MinParams: 9, MaxParams:  9}, // PS id color name area size type x y reach
		"ROLL":   {MinParams: 6, MaxParams:  6}, // ROLL from recip title result rlist id
		"SYNC":   {MinParams: 0, MaxParams:  2}, // SYNC [CHAT [target]]
		"TB":     {MinParams: 1, MaxParams:  1}, // TB state
		"TO":     {MinParams: 3, MaxParams:  4}, // TO from recip message [id]
		"/CONN":  {MinParams: 0, MaxParams:  0}, // /CONN
	}
}

func (ev *MapEvent) ValidateMapEvent() error {
	params, ok := map_event_checklist[ev.EventType()]
	if !ok {
		return fmt.Errorf("event type not understood")
	}
	if len(ev.Fields)-1 < params.MinParams || (params.MaxParams >= 0 && len(ev.Fields)-1 > params.MaxParams) {
		return fmt.Errorf("event %s has invalid parameter list (%d)", ev.EventType(), len(ev.Fields)-1)
	}
	return nil
}

// NewMapEvent creates a new map_event object from the raw data string
// received from a client. The parameters include:
//  raw - the raw data as received
//  
func NewMapEvent(raw, obj_id string, obj_class string) (*MapEvent, error) {
	fields, err := ParseTclList(raw)
	if err != nil {return nil, err}

	return NewMapEventFromList(raw, fields, obj_id, obj_class)
}

func NewMapEventFromList(raw string, fields []string, obj_id string, obj_class string) (*MapEvent, error) {
	var err error

	ev := new(MapEvent)
	ev.Class = obj_class
	ev.ID = obj_id
	ev.Fields = fields

	// Determine the "key" of the thing the event is modifying.
	// This is just something unique we can use to only keep the most
	// recent event in a chain of things where the previous ones are
	// outdated after the new one arrived. Thus, for attribute updates
	// to objects this includes the object ID and the attribute.

	err = ev.ValidateMapEvent()
	if err != nil {return nil, err}

	switch ev.EventType() {
		case "LS":
			// These are multi-line strings so they're a bit special. In this case,
			// we need the obj_id to be given to us explicitly rather than scanning through
			// all the raw data.
			ev.Key = "LS:" + ev.ID
		case "OA":
			// OA <id> <kvlist>
			// set the event ID from the data received
			if ev.ID == "" {
				ev.ID = ev.Fields[1]
			} else if ev.ID != ev.Fields[1] {
				return nil, fmt.Errorf("MapEvent: OA record is for object ID %s but applied to ID %s", ev.Fields[1], ev.ID)
			}
			// get the list of attributes we're changing here
			attr_val, err := ParseTclList(ev.Fields[2])
			if err != nil {
				return nil, fmt.Errorf("MapEvent: OA record's attribute list is malformed (%v)", err)
			}
			key_list := even_elements(attr_val)
			sort.Strings(key_list)
			ev.Key = "OA:" + ev.ID + ":" + strings.Join(key_list, ":")
			if ev.Class == "" {
				// let's try to guess the class based on what we can see here
				ev.Class = GuessObjectClass(key_list)
			}

		case "OA+", "OA-":
			// OA[+-] <id> <key> <vlist>
			// set the event ID from the data received
			if ev.ID == "" {
				ev.ID = ev.Fields[1]
			} else if ev.ID != ev.Fields[1] {
				return nil, fmt.Errorf("MapEvent: %s record is for object ID %s but applied to ID %s", ev.EventType(), ev.Fields[1], ev.ID)
			}
			// get the list of values we're changing here
			attr_val, err := ParseTclList(ev.Fields[3])
			if err != nil {
				return nil, fmt.Errorf("MapEvent: %s record's attribute list is malformed (%v)", ev.EventType(), err)
			}
			sort.Strings(attr_val)
			ev.Key = ev.EventType() + ":" + ev.ID + ":" + ev.Fields[2] + ":" + strings.Join(attr_val, ":")
			if ev.Class == "" {
				// let's try to guess the class based on what we can see here
				ev.Class = GuessObjectClass([]string{ev.Fields[2]})
			}
		case "CLR", "CLR@", "M?", "M@":
			ev.Key = ev.EventType() + ":" + ev.Fields[1]

		case "PS":
			// PS <id> <color> <name> <area> <size> player|monster <x> <y> <reach>
			// set the event ID from the data received
			if ev.ID == "" {
				ev.ID = ev.Fields[1]
			} else if ev.ID != ev.Fields[1] {
				return nil, fmt.Errorf("MapEvent: %s record is for object ID %s but applied to ID %s", ev.EventType(), ev.Fields[1], ev.ID)
			}
			ev.Key = "PS:" + ev.ID
			switch ev.Fields[6] {
				case "player":  ev.Class = "P"
				case "monster": ev.Class = "M"
				default:
					return nil, fmt.Errorf("MapEvent: PS record for %s has class %s which is undefined.", ev.ID, ev.Fields[6])
			}

		case "AV", "CO", "CS", "I", "IL", "TB":
			// For these, we just need to remember the last one that happened.
			ev.Key = ev.EventType()
	}
	// for any other event type, we will keep the Key to "", which means we won't bother
	// tracking it for state.

	return ev, nil
}

func (ev *MapEvent) EventType() string {
	if len(ev.Fields) == 0 {
		return ""
	}
	return ev.Fields[0]
}

func (ev *MapEvent) EventKey() string {
	return ev.Key
}

func (ev *MapEvent) EventClass() string {
	return ev.Class
}

// For events which represent chat messages, they have a message ID
// somewhere in their parameter fields. Specifically:
//   0    1      2        3         4        5      6
//   CC   <user> <target> <id>
//   TO   <from> <to>     <message> <id>
//   ROLL <from> <to>     <title>   <result> <list> <id>
//

func (ev *MapEvent) MessageID() (int, error) {
	switch ev.EventType() {
		case "CC":	 return strconv.Atoi(ev.Fields[3])
		case "TO":   return strconv.Atoi(ev.Fields[4])
		case "ROLL": return strconv.Atoi(ev.Fields[6])
	}
	return 0, fmt.Errorf("Event of type %s is not a chat message event.", ev.EventType())
}

func (ev *MapEvent) RawEventText() (string, error) {
	return ToTclString(ev.Fields)
}

//
// Can we send this event to the given user name?
// 
func (ev *MapEvent) CanSendTo(recipient string) bool {
	to_all := false
	if ev.EventType() == "TO" || ev.EventType() == "ROLL" {
		to_list, err := ParseTclList(ev.Fields[2])
		if err != nil {
			return false
		}
		for _, person := range to_list {
			if person == "*" {
				to_all = true
			} else if person == "%" {
				return recipient == "GM"
			} else {
				if person == recipient {
					return true
				}
			}
		}
		return to_all
	}

	return true
}


// Assign a new sequential ID to a chat-type event

func NextMessageID() string {
	message_id_lock.Lock()
	this_id := strconv.Itoa(next_message_id)
	next_message_id++
	message_id_lock.Unlock()
	return this_id
}

func (ev *MapEvent) AssignMessageID() {
	this_id := NextMessageID()
	switch ev.EventType() {
		case "CC":	 ev.Fields[3] = this_id
		case "TO":   ev.Fields[4] = this_id
		case "ROLL": ev.Fields[6] = this_id
	}
}



func GuessObjectClass(attr_list []string) string {
	for _, a := range attr_list {
		switch a {
			case "ANCHOR", "AOESHAPE", "ARCMODE", "ARROW",
				 "DASH", "EXTENT", "FILL", "FONT", "GROUP",
				 "HIDDEN", "IMAGE", "JOIN", "LAYER", "LEVEL",
				 "LINE", "POINTS", "SPLINE", "START",
				 "WIDTH", "X", "Y", "Z":
				return "E"
			case "AOE", "AREA", "COLOR", "DIM", "ELEV",
			     "GX", "GY", "HEALTH", "KILLED", "MOVEMODE",
				 "NAME", "NOTE", "REACH", "SKIN",
				 "STATUSLIST":
				// This could be either a player (P) or a
				// monster (M). We'll guess monster because
				// hopefully we already know who the players
				// are so we would have the class already
				// defined.
				return "M"
		}
		// Note that SIZE is an ambiguous attribute so we
		// can't base our guess solely on that.
	}
	return ""
}
