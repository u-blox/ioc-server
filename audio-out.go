/* Audio output (HTTP server) for the Internet of Chuffs.
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

// Some code here is based on the very instructive slides:
// https://www.slideshare.net/gamzabaw/implementing-hls-server-with-go
// by Sangwon Lee.

package main

import (
    "fmt"
    "log"
    "time"
    "net/http"
    "os"
    "path/filepath"
    "bytes"
    "sync"
    "container/list"
//    "github.com/gorilla/mux"
)

//--------------------------------------------------------------------
// Types 
//--------------------------------------------------------------------

// Description of an MP3 audio file
type Mp3AudioFile struct {
    fileName string
    title string
    timestamp time.Time
    duration time.Duration
    usable bool
}

//--------------------------------------------------------------------
// Constants
//--------------------------------------------------------------------

// The age at which an MP3 file should no longer be used
const MP3_MAX_AGE time.Duration = time.Minute

// The extension of an HLS index file
const HLS_INDEX_EXTENSION string = ".m3u8"

// The name of the index file
const HLS_INDEX_NAME string = "chuffs" + HLS_INDEX_EXTENSION

//--------------------------------------------------------------------
// Variables
//--------------------------------------------------------------------

// The control channel for media streaming out to users
var MediaControlChannel chan<- interface{}

// List of output MP3 files
var mp3FileList = list.New()

// Mutex to manage access to the index file
var indexAccess sync.Mutex

//--------------------------------------------------------------------
// Functions
//--------------------------------------------------------------------

// Add the cross-domain items to a response
// The options allowed are taken from:
// https://metajack.im/2010/01/19/crossdomain-ajax-for-xmpp-http-binding-made-easy/
func addCrossDomainToResponse(out http.ResponseWriter) {
    out.Header().Set("Access-Control-Allow-Origin", "*")
    out.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
    out.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Requested-With")
    out.Header().Set("Access-Control-Max-Age", "86400")
}

// Capture a cross-domain browsing OPTIONS request and allow it, returning
// true if this was a cross domain request.
func filterCrossDomainRequest(out http.ResponseWriter, in *http.Request) bool {
    var isCrossDomainRequest bool
    
    if (in.Method == "OPTIONS") {
        log.Printf("Received OPTIONS request from (%s), allowing it.\n", in.URL)
        addCrossDomainToResponse(out)
        out.WriteHeader(http.StatusOK)
        isCrossDomainRequest = true
    }
    
    return isCrossDomainRequest
}

// Return a time string in ISO8601 format in the UK timezone
func ukTimeIso8601(timestamp time.Time) string {
    location, _ := time.LoadLocation("Europe/London")
    return timestamp.In(location).Format("2006-01-02T15:04:05.000-07:00")    
}

// Create/update an index file
// See https://en.wikipedia.org/wiki/M3U
// and, in much more detail, https://tools.ietf.org/html/draft-pantos-http-live-streaming-17#section-4
func updateIndexFile(fileName string) {
    var maxDuration time.Duration
    var segmentData bytes.Buffer
    var numSegments int
    
    // Go through all of the MP3 files, assembling the segment
    // list and working out the dynamic header values
    for newElement := mp3FileList.Front(); newElement != nil; newElement = newElement.Next() {
        if newElement.Value.(*Mp3AudioFile).usable {
            numSegments++
            fmt.Fprintf(&segmentData, "#EXT-X-PROGRAM-DATE-TIME:%s\r\n", ukTimeIso8601(newElement.Value.(*Mp3AudioFile).timestamp))
            fmt.Fprintf(&segmentData, "#EXTINF:%f, %s\r\n", float32(newElement.Value.(*Mp3AudioFile).duration) / float32(time.Second),
                        newElement.Value.(*Mp3AudioFile).title)
            fmt.Fprintf(&segmentData, "%s\r\n\r\n", newElement.Value.(*Mp3AudioFile).fileName)
            if maxDuration < newElement.Value.(*Mp3AudioFile).duration {
                maxDuration = newElement.Value.(*Mp3AudioFile).duration
            }
        }
    }

    // Now lock access to the file and create it
    indexAccess.Lock()
    handle, err := os.Create(fileName)
    if err == nil {
        // Write the fixed header
        fmt.Fprintf(handle, "#EXTM3U\r\n")
        fmt.Fprintf(handle, "#EXT-X-VERSION:3\r\n")
        if numSegments > 0 {
            // Write the dynamic header fields
            fmt.Fprintf(handle, "#EXT-X-TARGETDURATION:%d\r\n", int(maxDuration / time.Second) + 1)
            fmt.Fprintf(handle, "#EXT-X-MEDIA-SEQUENCE:%d\r\n", numSegments - 1)
            // Write the segment list
            segmentData.WriteTo(handle)
        }
        log.Printf("Updated index file \"%s\" with %d segment(s).\n", fileName, numSegments)        
    } else {
        log.Printf("Unable to create index file \"%s\".\n", fileName)        
    }
    indexAccess.Unlock()
}

// Home page handler
func homeHandler (out http.ResponseWriter, in *http.Request, mp3Dir string) {
    http.Redirect(out, in, mp3Dir, http.StatusFound)
}

// Handle a stream request
func streamHandler(out http.ResponseWriter, in *http.Request) {
    if filepath.Ext(in.URL.Path) == HLS_INDEX_EXTENSION {        
        // Serve the HLS index file
        log.Printf("Serving index file \"%s\"...\n", in.URL.Path)
        indexAccess.Lock()
        http.ServeFile(out, in, in.URL.Path)
        indexAccess.Unlock()
        out.Header().Set("Content-Type","application/x-mpegurl")
        out.Header().Set("Cache-Control","no-cache")
    } else {
        // Serve the requested segment
        log.Printf("Serving segment file \"%s\"...\n", in.URL.Path)
        http.ServeFile(out, in, in.URL.Path)
        out.Header().Set("Content-Type","audio/mpeg")
    }
}

// Empty the MP3 file list, deleting the files as it goes
func clearMp3FileList(mp3Dir string) {
    log.Printf("Clearing MP3 file list...\n")
    for newElement := newDatagramList.Front(); newElement != nil; newElement = newElement.Next() {
        filePath := mp3Dir + string(os.PathSeparator) + newElement.Value.(*Mp3AudioFile).fileName        
        log.Printf("Deleting file \"%s\"...\n", filePath)
        err:= os.Remove(filePath)
        if err != nil {
            log.Printf("Unable to delete \"%s\".\n", filePath)
        }
        newDatagramList.Remove(newElement)
    }
}

// Start HTTP server for streaming output; this function should never return
func operateAudioOut(port string, mp3Dir string) {
    var channel = make(chan interface{})
    var err error
    var mp3IndexFilePath string
    streamTicker := time.NewTicker(time.Second * 5)
    
    MediaControlChannel = channel
    
    // Initialise the linked list of MP3 output files
    mp3FileList.Init()
    
    // Set up the index file path
    mp3IndexFilePath = mp3Dir + string(os.PathSeparator) + HLS_INDEX_NAME
    
    // Create an initial (empty) index file
    updateIndexFile(mp3IndexFilePath)

    // Timed function to perform operations on the stream
    go func() {
        for _ = range streamTicker.C {
            // Go through the file list and mark old files as unusable, attempting
            // to delete unusable files as we go 
            for newElement := mp3FileList.Front(); newElement != nil; newElement = newElement.Next() {
                if (newElement.Value.(*Mp3AudioFile).usable) && (time.Now().Sub(newElement.Value.(*Mp3AudioFile).timestamp) > MP3_MAX_AGE) {
                    //newElement.Value.(*Mp3AudioFile).usable = false;
                    log.Printf ("MP3 file \"%s\", received at %s, now out of date (time now is %s).\n",
                                newElement.Value.(*Mp3AudioFile).fileName, newElement.Value.(*Mp3AudioFile).timestamp.String(),
                                time.Now().String())
                    updateIndexFile(mp3IndexFilePath)
                }
                if !newElement.Value.(*Mp3AudioFile).usable {
                    filePath := mp3Dir + string(os.PathSeparator) + newElement.Value.(*Mp3AudioFile).fileName
                    if os.Remove(filePath) == nil {
                        log.Printf ("MP3 file \"%s\" successfully deleted and will be removed from the list.\n", filePath)
                        mp3FileList.Remove(newElement)
                    }
                }
            }
        }        
    }()
    
    // Process media control commands
    go func() {
        for cmd := range channel {
            switch message := cmd.(type) {
                // Handle the media control messages
                case *Mp3AudioFile:
                {
                    log.Printf("Adding new MP3 file \"%s\", duration %d millisecond(s), to the FIFO list...\n", message.fileName, int(message.duration / time.Millisecond))
                    mp3FileList.PushBack(message)
                    updateIndexFile(mp3IndexFilePath)
                }
            }
        }
        clearMp3FileList(mp3Dir)
        fmt.Printf("HTTP streaming channel closed, stopping.\n")
    }()
    
    // Set up the HTTP page handlers
    http.HandleFunc("/", func(out http.ResponseWriter, in *http.Request) {
        if !filterCrossDomainRequest(out, in) {
            addCrossDomainToResponse(out)
            homeHandler(out, in, mp3Dir + "/")
        }
    })
    http.HandleFunc(mp3Dir + "/", func(out http.ResponseWriter, in *http.Request) {
        if !filterCrossDomainRequest(out, in) {
            addCrossDomainToResponse(out)
            streamHandler(out, in)
        }
    })

    fmt.Printf("Starting HTTP server for Chuff requests on port %s.\n", port)
    
    // Start the HTTP server (should block)
    err = http.ListenAndServeTLS(":" + port, "cert.pem", "privkey.pem", nil)
    
    if err != nil {        
        fmt.Fprintf(os.Stderr, "Could not start HTTP server (%s).\n", err.Error())
    }
}

/* End Of File */
