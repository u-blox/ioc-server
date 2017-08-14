/* main() for Internet of Chuffs server.
 *
 * Copyright (C) u-blox Melbourn Ltd
 * u-blox Melbourn Ltd, Melbourn, UK
 *
 * All rights reserved.
 *
 * This source file is the sole property of u-blox Melbourn Ltd.
 * Reproduction or utilization of this source in whole or part is
 * forbidden without the written consent of u-blox Melbourn Ltd.
 */

package main

import (
    "fmt"
    "os"
    "log"
    "github.com/jessevdk/go-flags"
//    "encoding/hex"
)

// This is the Internet of Chuffs, server side.
// The input stream from the IoC client is 16-bit PCM
// audio sampled at 16 kHz, arriving in 20 ms blocks
// that include a sequence number and timestamp.
// This is written to a buffer and then LAME
// (lame.sourceforge.net) is employed to produce
// an MP3 stream that is streamed out over HTTP.

//--------------------------------------------------------------------
// Types
//--------------------------------------------------------------------

//--------------------------------------------------------------------
// Variables
//--------------------------------------------------------------------

// Command-line items
var opts struct {
    Ports struct {
        In string `positional-arg-name:"input-port"`
        Out string `positional-arg-name:"output-port"`
    } `positional-args:"true" required:"yes"`
    Mp3Dir string `short:"m" long:"mp3dir" default:"." description:"directory where mp3 audio files will be stored (must exist)"`
    LogName string `short:"l" long:"logfile" description:"file for logging output (will be truncated if it already exists)"`
    PcmName string `short:"p" long:"pcmfile" description:"file for 16 bit PCM output (will be truncated if it already exists)"`
}

//--------------------------------------------------------------------
// Functions
//--------------------------------------------------------------------

// Deal with command-line parameters
func cli() {
     _, err := flags.Parse(&opts)

    if err != nil {
        os.Exit(-1)        
    }    
}

// Entry point
func main() {
    var pcmHandle *os.File
    var logHandle *os.File
    var stat os.FileInfo
    var err error

    // Handle the command line
    cli()
    
    // Say what we're doing
    if opts.LogName != "" {
        logHandle, err = os.Create(opts.LogName);
        // Point logging at the right place
        if logHandle != nil {
            defer logHandle.Close()
            log.SetOutput(logHandle)
        }        
    }    
    if (opts.PcmName != "") && (err == nil) {
        log.Printf("Opening %s for raw PCM output.\n", opts.PcmName)        
        pcmHandle, err = os.Create(opts.PcmName);
    }
    if err == nil {
        defer pcmHandle.Close()
        
        // Run the audio processing loop
        go operateAudioProcessing(pcmHandle, opts.Mp3Dir)
        
        // Run the UDP server loop for incoming audio
        go operateAudioIn(opts.Ports.In)
        
        // Run the HTTP server for audio output (which should block)
        operateAudioOut(opts.Ports.Out)
    } else {
        if (opts.PcmName != "") && (pcmHandle == nil) {
            fmt.Fprintf(os.Stderr, "Unable to open %s for raw PCM output (%s).\n", opts.PcmName, err.Error())
        }
        if (opts.LogName != "") && (logHandle == nil) {
            fmt.Fprintf(os.Stderr, "Unable to open %s for logging output (%s).\n", opts.LogName, err.Error())
        }
        os.Exit(-1)
    }                
}
