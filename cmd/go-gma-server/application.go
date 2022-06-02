package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/lestrrat-go/strftime"
)

//
// Application holds the global settings and other context for the application
// generally.
//
type Application struct {
	// Logger is whatever device or file we're writing logs to.
	Logger *log.Logger

	// If DebugLevel is 0, no extra debugging output will be logged.
	// Otherwise, increasing levels of output are generated for
	// increasing values of DebugLevel.
	DebugLevel int

	// Endpoint is the "[host]:port" string which specifies where our
	// incoming socket is listening.
	Endpoint string

	// If not empty, this gives the filename from which we are to read in
	// the initial client command set.
	InitFile string

	// If not empty, we require authentication, with passwords taken
	// from this file.
	PasswordFile string

	// How often to save internal state to disk.
	SaveInterval time.Duration

	// Pathname for database file.
	DatabaseName string
}

//
// Debug logs messages conditionally based on the currently set
// debug level. It acts just like fmt.Println as far as formatting
// its arguments.
//
func (a *Application) Debug(level int, message ...any) {
	if a != nil && a.Logger != nil && a.DebugLevel >= level {
		a.Logger.Println(message...)
	}
}

//
// Log logs messages to the application's logger.
// It acts just like fmt.Println as far as formatting
// its arguments.
//
func (a *Application) Log(message ...any) {
	if a != nil && a.Logger != nil {
		a.Logger.Println(message...)
	}
}

//
// Logf logs messages to the application's logger.
// It acts just like fmt.Printf as far as formatting
// its arguments.
//
func (a *Application) Logf(format string, args ...any) {
	if a != nil && a.Logger != nil {
		a.Logger.Printf(format, args...)
	}
}

//
// Debugf works like Debug, but takes a format string and argument
// list just like fmt.Printf does.
//
func (a *Application) Debugf(level int, format string, args ...any) {
	if a != nil && a.Logger != nil && a.DebugLevel >= level {
		a.Logger.Printf(format, args...)
	}
}

//
// GetAppOptions configures the application by reading command-line options.
//
func (a *Application) GetAppOptions() error {

	var initFile = flag.String("init-file", "", "Load initial client commands from named file path")
	var logFile = flag.String("log-file", "-", "Write log to given pathname (stderr if '-'); special % tokens allowed in path")
	var passFile = flag.String("password-file", "", "Require authentication with named password file")
	var endPoint = flag.String("endpoint", ":2323", "Incoming connection endpoint ([host]:port)")
	var saveInterval = flag.String("save-interval", "10m", "Save internal state this often")
	var sqlDbName = flag.String("sqlite", "", "Specify filename for sqlite database to use")
	flag.Parse()

	if *logFile == "" {
		a.Logger = nil
	} else {
		a.Logger = log.Default()
		if *logFile != "-" {
			path, err := a.FancyFileName(*logFile)
			if err != nil {
				return fmt.Errorf("unable to understand log file path \"%s\": %v", *logFile, err)
			}
			f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				return fmt.Errorf("unable to open log file: %v", err)
			} else {
				a.Logger.SetOutput(f)
			}
		}
	}

	if *initFile != "" {
		a.InitFile = *initFile
		a.Logf("reading client initial command set from \"%s\"", a.InitFile)
	}

	if *passFile != "" {
		a.PasswordFile = *passFile
		a.Logf("authentication enabled via \"%s\"", a.PasswordFile)
	} else {
		a.Log("WARNING: authentication not enabled!")
	}

	if *endPoint != "" {
		a.Endpoint = *endPoint
		a.Logf("configured to listen on \"%s\"", a.Endpoint)
	} else {
		return fmt.Errorf("non-empty tcp [host]:port value required")
	}

	if *saveInterval == "" {
		a.SaveInterval = 10 * time.Minute
		a.Logf("defaulting state save interval to 10 minutes")
	} else {
		d, err := time.ParseDuration(*saveInterval)
		if err != nil {
			return fmt.Errorf("invalid save-time interval: %v", err)
		}
		a.SaveInterval = d
		a.Logf("saving state to disk every %v", a.SaveInterval)
	}

	if *sqlDbName == "" {
		return fmt.Errorf("database name is required")
	}
	a.DatabaseName = *sqlDbName
	a.Logf("using database \"%s\" to store internal state", a.DatabaseName)

	return nil
}

//
// FancyFileName expands tokens found in the path string to allow the user
// to specify dynamically-named files at runtime. If there's a problem with
// the formatting, an error is returned along with the original path.
//
// The tokens which may appear in the path include the following
// (note that all of these are modified as appropriate to the locale's
// national conventions and language):
//    %A   full weekday name
//    %a   abbreviated weekday name
//    %B   full month name
//    %b   abbreviated month name
//    %C   zero-padded two-digit year 00-99
//    %c   time and date
//    %d   day of month as number 01-31 (zero padded)
//    %e   day of month as number  1-31 (space padded)
//    %F   == %Y-%m-%d
//    %H   hour as number 00-23 (zero padded)
//    %h   abbreviated month name (same as %b)
//    %I   hour as number 01-12 (zero padded)
//    %j   day of year as number 001-366
//    %k   hour as number  0-23 (space padded)
//    %L   milliseconds as number 000-999
//    %l   hour as number  1-12 (space padded)
//    %M   minute as number 00-59
//    %m   month as number 01-12
//    %P   process ID
//    %p   AM or PM
//    %R   == %H:%M
//    %r   == %I:%M:%S %p
//    %S   second as number 00-60
//    %s   Unix timestamp as a number
//    %T   == %H:%M:%S
//    %U   week of the year as number 00-53 (Sunday as first day of week)
//    %u   weekday as number (1=Monday .. 7=Sunday)
//    %V   week of the year as number 00-53 (Monday as first day of week)
//    %v   == %e-%b-%Y
//    %W   week of the year as number 00-53 (Monday as first day of week)
//    %w   weekday as number (0=Sunday .. 6=Saturday)
//    %X   time
//    %x   date
//    %Y   full year
//    %y   two-digit year (00-99)
//    %Z   time zone name
//    %z   time zone offset from UTC
//    %µ   microseconds as number 000-999
//    %%   literal % character
//
func (a *Application) FancyFileName(path string) (string, error) {
	ss := strftime.NewSpecificationSet()

	if err := ss.Delete('n'); err != nil {
		return path, err
	}
	if err := ss.Delete('t'); err != nil {
		return path, err
	}
	if err := ss.Delete('D'); err != nil {
		return path, err
	}
	if err := ss.Set('P', strftime.Verbatim(strconv.Itoa(os.Getpid()))); err != nil {
		return path, err
	}

	return strftime.Format(path, time.Now(),
		strftime.WithSpecificationSet(ss),
		strftime.WithUnixSeconds('s'),
		strftime.WithMilliseconds('L'),
		strftime.WithMicroseconds('µ'),
	)

}
