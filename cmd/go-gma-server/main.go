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
//                                     MapService                                     //
//                                                                                    //
// Inter-map communication service.  Transmits map updates to other maps and allows   //
// API callers to inject events to be sent to all maps.                               //
//                                                                                    //
// This is a re-implementation from scratch of the Python GMA game server (as         //
// originally implemented in the Mapper.MapService module), in the Go language, in    //
// the hopes that this will provide better performance than the Python version.  It   //
// was also done out of personal interest to explore Go design features for a server  //
// such as this one.                                                                  //
//                                                                                    //
////////////////////////////////////////////////////////////////////////////////////////

package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/MadScienceZone/go-gma/v5/mapper"
	"github.com/newrelic/go-agent/v3/newrelic"

	_ "github.com/mattn/go-sqlite3"
)

//
// Auto-configured values
//

const GMAVersionNumber = "4.0.0" // @@##@@

//
// eventMonitor responds to signals and timers that affect our overall operation
// independent of client requests.
//
/*
func eventMonitor(sig_chan chan os.Signal, stop_chan chan int, ms *mapservice.MapService, saveInterval int) {
	report_interval := 1
	var save_signal *time.Ticker

	if saveInterval <= 0 {
		save_signal = time.NewTicker(1 * time.Minute)
		save_signal.Stop()
		log.Printf("Save interval specified as %d; periodic saves DISABLED", saveInterval)
	} else {
		save_signal = time.NewTicker(time.Duration(saveInterval) * time.Minute)
	}
	ping_signal := time.NewTicker(1 * time.Minute)

	if ms.Database == nil {
		log.Printf("No database open; periodic saves DISABLED")
		save_signal.Stop()
	}

	for {
		select {
		case s := <-sig_chan:
			log.Printf("Received signal %v", s)
			switch s {
			case syscall.SIGHUP:
				stop_chan <- 1

			case syscall.SIGUSR1:
				ms.DumpState()

			case syscall.SIGUSR2:
				log.Printf("**SAVE** due to signal")
				if err := ms.SaveState(); err != nil {
					log.Printf("Error saving game state: %v", err)
				}

			case syscall.SIGINT:
				// Make a quick effort to shut down as fast as possible
				// by terminating all client connections immediately.
				log.Printf("EMERGENCY SHUTDOWN INITIATED")
				ms.AcceptIncoming = false
				for i, client := range ms.Clients {
					log.Printf("Terminating client %v from %s", i, client.ClientAddr)
					client.Connection.Close()
				}
				stop_chan <- 1
			}

		case t := <-save_signal.C:
			// suppress messages and unnecessary saves
			// if we're idling
			if ms.SaveNeeded || report_interval <= 1 {
				log.Printf("***SAVE*** due to timer %v", t)
				if err := ms.SaveState(); err != nil {
					log.Printf("Error saving game state: %v", err)
				}
			}

		case <-ping_signal.C:
			any_connections := ms.PingAll()
			if any_connections {
				if report_interval > 1 {
					report_interval = 1
					log.Printf("Activity detected; reset ping timer to 1 minute")
					ping_signal.Reset(1 * time.Minute)
				}
			} else {
				new_interval := report_interval * 2
				if new_interval > 60 {
					new_interval = 60
				}
				if new_interval != report_interval {
					report_interval = new_interval
					log.Printf("No connections; backing off ping timer to %d minutes", new_interval)
					ping_signal.Reset(time.Duration(new_interval) * time.Minute)
				}
			}
		}
	}
}
*/

func main() {
	var nrApp *newrelic.Application
	var err error

	app := Application{
		Logger: log.Default(),
	}
	app.Logger.SetPrefix("go-gma-server: ")
	if err := app.GetAppOptions(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal error: %v\n", err)
		os.Exit(1)
	}
	app.Logf("Server %s started", GMAVersionNumber)
	app.Logf("Implements protocol %d (minimum %d, maximum %d)",
		mapper.GMAMapperProtocol,
		mapper.MinimumSupportedMapProtocol,
		mapper.MaximumSupportedMapProtocol)

	/* instrumentation */
	// set the following environment variables for the New Relic
	// Go Agent:
	//    NEW_RELIC_APP_NAME = the name you want to appear in the datasets
	//    NEW_RELIC_LICENSE_KEY = your license key
	//
	if InstrumentCode {
		nrApp, err = newrelic.NewApplication(
			newrelic.ConfigAppName("go-gma-server"),
			newrelic.ConfigFromEnvironment(),
			newrelic.ConfigDebugLogger(os.Stdout),
		)
		if err != nil {
			app.Logf("unable to start instrumentation: %v", err)
			os.Exit(1)
		}
		defer func() {
			app.Logf("waiting for instrumentation to finish (max 30 sec) ...")
			nrApp.Shutdown(30 * time.Second)
		}()
	}

	for {
		func() {
			if InstrumentCode {
				defer nrApp.StartTransaction("testing").End()
			}
			time.Sleep(10 * time.Second)
		}()
	}

	// TODO instrumentation
	/*
		txn := nrapp.StartTransaction("background")
		defer txn.End()
		// do stuff
	*/

	/*
		GMAMapperProtocol           = 400      // @@##@@ auto-configured
		GMAVersionNumber            = "4.3.12" // @@##@@ auto-configured
		MinimumSupportedMapProtocol = 400
		MaximumSupportedMapProtocol = 400
	*/
	/*
		var GMAVersionNumber, GMAMapperProtocol string
		var err error

		// Automatically generated version numbers
		GMAVersionNumber = "4.2.2" // @@##@@
		GMAMapperProtocol = "400"  // @@##@@

		passfile := flag.String("password-file", "", "get passwords from the designated file")
		port := flag.Int("port", 2323, "TCP port of map service")
		logfile := flag.String("log-file", "", "log connections and other info to this file")
		initfile := flag.String("init-file", "", "initial commands to send all clients upon authentication")
		greetfile := flag.String("greet-file", "", "initial server greeting sent to clients upon connection")
		saveint := flag.Int("save-interval", 10, "frequency at which to save game state")
		sqlitedb := flag.String("sqlite", "", "use the named sqlite3 database for persistent storage")
		mysqldb := flag.String("mysql", "", "use the named mysql database for persistent storage")
		flag.Parse()

		if *logfile != "" {
			lf, err := os.OpenFile(*logfile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				log.Fatalf("Unable to open logfile \"%s\": %v", *logfile, err)
				os.Exit(2)
			}
			log.SetOutput(lf)
			defer lf.Close()
		}

		if GMAMapperProtocol != mapservice.PROTOCOL_VERSION {
			log.Printf("WARNING! This server implements service protocol version %s but %s is the current protocol for the GMA tool suite!\n",
				mapservice.PROTOCOL_VERSION, GMAMapperProtocol)
			expected_proto, err := strconv.Atoi(GMAMapperProtocol)
			supported_proto, err2 := strconv.Atoi(mapservice.PROTOCOL_VERSION)
			if err != nil || err2 != nil {
				log.Fatal("FATAL! Unable to parse version numbers as integers! Bailing out now.")
				os.Exit(1)
			}

			if expected_proto < supported_proto {
				log.Printf("=======  Since our version is higher, this may be an experimental new server.")
			} else {
				log.Printf("=======  This probably means this is an old version of the mapservice program.")
			}
		}

		log.Printf("GMA Map Service (golang), version %s\n", GMAVersionNumber)

		// open database
		var sqldb *sql.DB
		if *sqlitedb != "" {
			if *mysqldb != "" {
				log.Fatalf("You can't specify --sqlite and --mysql at the same time.")
				os.Exit(1)
			}
			if _, err = os.Stat(*sqlitedb); os.IsNotExist(err) {
				// database doesn't exist yet; create a new one

				sqldb, err = sql.Open("sqlite3", "file:"+*sqlitedb)
				log.Printf("No existing sqlite3 database \"%s\" found--creating a new one.", *sqlitedb)
				if err != nil {
					log.Fatalf("Unable to create sqlite3 database %s: %v", *sqlitedb, err)
					os.Exit(2)
				}
				_, err = sqldb.Exec(`
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
					create table events (
						eventid integer primary key,
						rawdata  text    not null,
						sequence integer not null,
						key      text    not null,
						class    text    not null,
						objid    text    not null
					);
					create table extradata (
						extraid integer primary key,
						eventid integer not null,
						datarow text    not null,
							foreign key (eventid)
								references events (eventid)
								on delete cascade
					);
					create table chats (
						rawdata text    not null,
						msgid   text    not null
					);
					create table images (
						name    text    not null,
						zoom    text    not null,
						location text   not null
					);
					create table idbyname (
						name    text    not null,
						objid	text    not null
					);
					create table classbyid (
						objid   text    not null,
						class   text    not null
					);`)
				if err != nil {
					log.Printf("Unable to create sqlite3 database %s contents: %v", *sqlitedb, err)
					log.Fatalf("WARNING! %s may be in a corrupt state--fix or delete before running the server!", *sqlitedb)
					os.Exit(2)
				}
			} else {
				sqldb, err = sql.Open("sqlite3", "file:"+*sqlitedb)
			}
			defer sqldb.Close()
		} else if *mysqldb != "" {
			log.Fatalf("--mysql not yet implemented.")
			os.Exit(1)
		} else {
			log.Printf("WARNING: No database back-end specified. No persistent data storage will be used!")
			log.Printf("(Avoid this by specifying the --sqlite=<filename> option)")
			sqldb = nil
		}

		// set up authentication
		var groupPassword []byte
		var gmPassword []byte
		personalPasswords := make(map[string][]byte)

		if *passfile != "" {
			// The password file contains the group password
			// and (optionally) gm password. Other personal
			// ones are stored in the database (if they are used)
			fp, err := os.Open(*passfile)
			if err != nil {
				log.Fatalf("Unable to open password file \"%s\": %v", *passfile, err)
				os.Exit(2)
			}
			scanner := bufio.NewScanner(fp)
			if scanner.Scan() {
				groupPassword = []byte(scanner.Text())
				if scanner.Scan() {
					gmPassword = []byte(scanner.Text())
					//
					// continue reading personal passwords if any
					//
					line := 3
					for scanner.Scan() {
						personal_password := strings.SplitN(scanner.Text(), ":", 2)
						if len(personal_password) != 2 {
							log.Printf("Warning: Rejecting personal password setting on line %d (missing ':' delimiter)", line)
						} else {
							log.Printf("Set personal password for %s", personal_password[0])
							personalPasswords[personal_password[0]] = []byte(personal_password[1])
						}
					}
				}
			}
			fp.Close()
		}

		// start listening to incoming port
		incoming, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
		if err != nil {
			log.Fatalf("Unable to open incoming TCP port %d: %v", *port, err)
			os.Exit(2)
		}
		log.Printf("Listening on port %d", *port)
		defer incoming.Close()

		// signal handler
		sig_channel := make(chan os.Signal, 1)
		stop_channel := make(chan int, 1)
		signal.Notify(sig_channel, syscall.SIGHUP, syscall.SIGUSR1, syscall.SIGUSR2, syscall.SIGINT)

		ms := mapservice.MapService{
			IncomingListener:  incoming,
			Database:          sqldb,
			PlayerGroupPass:   groupPassword,
			GmPass:            gmPassword,
			PersonalPasswords: personalPasswords,
			Clients:           make(map[string]*mapservice.MapClient),
			InitFile:          *initfile,
			GreetFile:         *greetfile,
			EventHistory:      make(map[string]*mapservice.MapEvent),
			ImageList:         make(map[string]string),
			StopChannel:       stop_channel,
		}
		go ms.Run()
		go eventMonitor(sig_channel, stop_channel, &ms, *saveint)
		<-stop_channel
		log.Printf("Received STOP signal; shutting down")
		if err = ms.SaveState(); err != nil {
			log.Printf("Error trying to save game state before exit: %v", err)
		}
		ms.Shutdown()
		log.Printf("server shut down")
	*/
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
