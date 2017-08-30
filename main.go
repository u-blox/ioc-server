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
    "path/filepath"
    "strings"
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
// Constants
//--------------------------------------------------------------------

// The extension of an HLS playlist file
const PLAYLIST_EXTENSION string = ".m3u8"

// The extension used for audio segment files
const SEGMENT_EXTENSION string = ".ts"

//--------------------------------------------------------------------
// Variables
//--------------------------------------------------------------------

// Command-line items
var opts struct {
    Required struct {
        In string `positional-arg-name:"input-port" description:"the input port for incoming raw PCM chuffs"`
        Out string `positional-arg-name:"output-port" description:"the output port for HTTP service"`
        PlaylistPath string `positional-arg-name:"playlistpath" description:"path to the live playlist file (any file extension will be replaced with .m3u8); the playlist file will be created by this program and the audio files will be stored in the same directory as the playlist file.  THe HTML file that serves the playlist file should be placed in this directory."`
    } `positional-args:"true" required:"yes"`
    ClearTsDir bool `short:"c" long:"clear" description:"clear the segment files from the live playlist directory before using it"`
    OOSDir string `short:"o" long:"oosdir" description:"the path to a directory containing HTML and, optionally in the same directory, static playlist/audio files, to use when there is no live audio to stream (you must create these files yourself)"`
    LogName string `short:"l" long:"logfile" description:"file for logging output (will be truncated if it already exists)"`
    RawPcmName string `short:"r" long:"rawpcmfile" description:"file for 16 bit PCM output (will be truncated if it already exists)"`
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
    var rawPcmHandle *os.File
    var logHandle *os.File
    var err error
    var mp3Dir string
    var playlistPath string

    // Handle the command line
    cli()
    
    // Open the log and raw PCM files
    if opts.LogName != "" {
        logHandle, err = os.Create(opts.LogName);
        // Point logging at the right place
        if logHandle != nil {
            defer logHandle.Close()
            log.SetOutput(logHandle)
        }        
    }    
    if (opts.RawPcmName != "") && (err == nil) {
        log.Printf("Opening \"%s\" for raw PCM output.\n", opts.RawPcmName)        
        rawPcmHandle, err = os.Create(opts.RawPcmName);
    }
    
    // Get the directory in which to store MP3 files and the playlist file path
    mp3Dir = filepath.Dir(opts.Required.PlaylistPath)
    playlistPath = strings.TrimSuffix(opts.Required.PlaylistPath, filepath.Ext(opts.Required.PlaylistPath)) + PLAYLIST_EXTENSION
    
    // Clear the TS files from the live playlist directory
    if mp3Dir != "" {
        _ = os.MkdirAll(mp3Dir, os.ModePerm)
        if (opts.ClearTsDir) && (err == nil) {
            log.Printf("Clearing %s files from directory \"%s\".\n", SEGMENT_EXTENSION, mp3Dir)
            segmentFiles, err1 := filepath.Glob(mp3Dir + string(os.PathSeparator) + "*" + SEGMENT_EXTENSION)
            if err1 == nil {
                for _, segmentFile := range segmentFiles {
                    err1 = os.Remove(segmentFile)
                    if err1 != nil {
                        log.Printf("Unable to delete file \"%s\" (%s).\n", segmentFile, err1.Error())
                    }
                }
            } else {
                log.Printf("Unable to delete %s files (%s).\n", SEGMENT_EXTENSION, err1.Error())
            }
        }
    } 
    
    if err == nil {
        defer rawPcmHandle.Close()
        
        // Run the audio processing loop
        go operateAudioProcessing(rawPcmHandle, mp3Dir)
        
        // Run the UDP server loop for incoming audio
        go operateAudioIn(opts.Required.In)
        
        // Run the HTTP server for audio output (which should block)
        operateAudioOut(opts.Required.Out, playlistPath, opts.OOSDir)
    } else {
        if (opts.RawPcmName != "") && (rawPcmHandle == nil) {
            fmt.Fprintf(os.Stderr, "Unable to open %s for raw PCM output (%s).\n", opts.RawPcmName, err.Error())
        }
        if (opts.LogName != "") && (logHandle == nil) {
            fmt.Fprintf(os.Stderr, "Unable to open %s for logging output (%s).\n", opts.LogName, err.Error())
        }
        os.Exit(-1)
    }
}
