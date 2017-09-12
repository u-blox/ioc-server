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
    "math"
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
    removable bool
}

//--------------------------------------------------------------------
// Constants
//--------------------------------------------------------------------

// The age at which an MP3 file should no longer be used
const MP3_USABLE_AGE time.Duration = time.Minute * 2

// The age at which an MP3 file can be deleted
const MP3_REMOVABLE_AGE time.Duration = time.Minute * 5

// The lag from the newest point in the playlist to the point
// where a browser should begin playing from the playlist
const MAX_PLAY_LAG time.Duration = time.Second * 20

//--------------------------------------------------------------------
// Variables
//--------------------------------------------------------------------

// The control channel for media streaming out to users
var MediaControlChannel chan<- interface{}

// List of output MP3 files
var mp3FileList = list.New()

// Mutex to manage access to the playlist file
var playlistAccess sync.Mutex

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

// Create/update the playlist file
// See https://en.wikipedia.org/wiki/M3U
// and, in much more detail, https://tools.ietf.org/html/draft-pantos-http-live-streaming-17#section-4
func updatePlaylistFile(fileName string, mediaSequenceNumber int) bool {
    var maxSegmentDuration time.Duration
    var segmentData bytes.Buffer
    var numSegments int
    var totalDuration time.Duration
    
    // Go through all of the MP3 files, assembling the segment
    // list and working out the dynamic header values
    for newElement := mp3FileList.Front(); newElement != nil; newElement = newElement.Next() {
        if newElement.Value.(*Mp3AudioFile).usable {
            numSegments++
            fmt.Fprintf(&segmentData, "#EXT-X-PROGRAM-DATE-TIME:%s\r\n", ukTimeIso8601(newElement.Value.(*Mp3AudioFile).timestamp))
            fmt.Fprintf(&segmentData, "#EXTINF:%f, %s\r\n", float32(newElement.Value.(*Mp3AudioFile).duration) / float32(time.Second),
                        newElement.Value.(*Mp3AudioFile).title)
            fmt.Fprintf(&segmentData, "%s\r\n", newElement.Value.(*Mp3AudioFile).fileName)
            totalDuration += newElement.Value.(*Mp3AudioFile).duration
            if maxSegmentDuration < newElement.Value.(*Mp3AudioFile).duration {
                maxSegmentDuration = newElement.Value.(*Mp3AudioFile).duration
            }
        }
    }
    
    // Now lock access to the file and create it
    playlistAccess.Lock()
    handle, err := os.Create(fileName)
    if err == nil {
        // Write the fixed header
        fmt.Fprintf(handle, "#EXTM3U\r\n")
        fmt.Fprintf(handle, "#EXT-X-VERSION:3\r\n")
        if numSegments > 0 {
            // Write the dynamic header fields
            fmt.Fprintf(handle, "#EXT-X-TARGETDURATION:%d\r\n", int(math.Ceil(float64(maxSegmentDuration) / float64(time.Second))))
            fmt.Fprintf(handle, "#EXT-X-MEDIA-SEQUENCE:%d\r\n", mediaSequenceNumber)
            if totalDuration > MAX_PLAY_LAG {
                fmt.Fprintf(handle, "#EXT-X-START:TIME-OFFSET=-%f\r\n", float32(MAX_PLAY_LAG) / float32(time.Second))
            }
            // Write the segment list
            segmentData.WriteTo(handle)
        }
        log.Printf("Updated playlist file \"%s\" with %d segment(s).\n", fileName, numSegments)
        handle.Close()        
    } else {
        log.Printf("Unable to create playlist file \"%s\" (%s).\n", fileName, err.Error())        
    }
    playlistAccess.Unlock()
    
    return err == nil
}

// Home page handler
func homeHandler (out http.ResponseWriter, in *http.Request, newPath string) {
    log.Printf("Home handler was asked for \"%s\", redirecting to \"%s\"...\n", in.URL.Path, newPath)
    http.Redirect(out, in, newPath, http.StatusFound)
}

// Handle a stream request
func streamHandler(out http.ResponseWriter, in *http.Request) {
    var ext string = filepath.Ext(in.URL.Path)
    
    log.Printf("Stream handler was asked for \"%s\"...\n", in.URL.Path)
    if ext == PLAYLIST_EXTENSION {        
        // Serve the playlist file
        log.Printf("Serving playlist file \"%s\".\n", in.URL.Path)
        playlistAccess.Lock()
        http.ServeFile(out, in, in.URL.Path)
        playlistAccess.Unlock()
        out.Header().Set("Content-Type","application/x-mpegurl")
        out.Header().Set("Cache-Control","no-cache")
    } else if ext == SEGMENT_EXTENSION {
        // Serve the requested segment
        log.Printf("Serving segment file \"%s\".\n", in.URL.Path)
        http.ServeFile(out, in, in.URL.Path)
        out.Header().Set("Content-Type","audio/mpeg")
        out.Header().Set("Cache-Control","no-cache")
    } else {
        // Just serve the requested page
        log.Printf("Serving \"%s\".\n", in.URL.Path)
        http.ServeFile(out, in, in.URL.Path)
        out.Header().Set("Cache-Control","no-cache")
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
func operateAudioOut(port string, playlistPath string,  oOSDir string) {
    var channel = make(chan interface{})
    var err error
    var mp3Dir string
    var mediaSequenceNumber int
    var oOS bool = true
    streamTicker := time.NewTicker(time.Second * 5)
    mux := http.NewServeMux()
    
    MediaControlChannel = channel
    
    // Initialise the linked list of MP3 output files
    mp3FileList.Init()
    
    // Set up the MP3 directory
    mp3Dir = filepath.Dir(playlistPath)
    
    // Create an initial (empty) playlist file    
    if !updatePlaylistFile(playlistPath, mediaSequenceNumber) {
        fmt.Fprintf(os.Stderr, "Unable to create playlist file \"%s\" (%s).\n", playlistPath, err.Error())
        os.Exit(-1)            
    }

    // Timed function to perform operations on the stream
    go func() {
        for _ = range streamTicker.C {
            // Go through the file list and mark old files as unusable, then removable, 
            // and attempt to delete removable files as we go 
            for newElement := mp3FileList.Front(); newElement != nil; newElement = newElement.Next() {
                if (newElement.Value.(*Mp3AudioFile).usable) && (time.Now().Sub(newElement.Value.(*Mp3AudioFile).timestamp) > MP3_USABLE_AGE) {
                    newElement.Value.(*Mp3AudioFile).usable = false;
                    mediaSequenceNumber++;
                    log.Printf ("MP3 file \"%s\", received at %s, no longer usable (time now is %s).\n",
                                newElement.Value.(*Mp3AudioFile).fileName, newElement.Value.(*Mp3AudioFile).timestamp.String(),
                                time.Now().String())
                    updatePlaylistFile(playlistPath, mediaSequenceNumber)
                }                
                if (!newElement.Value.(*Mp3AudioFile).usable) && (time.Now().Sub(newElement.Value.(*Mp3AudioFile).timestamp) > MP3_REMOVABLE_AGE) {
                    newElement.Value.(*Mp3AudioFile).removable = true;
                    log.Printf ("MP3 file \"%s\", received at %s, can now been deleted (time now is %s).\n",
                                newElement.Value.(*Mp3AudioFile).fileName, newElement.Value.(*Mp3AudioFile).timestamp.String(),
                                time.Now().String())
                }                
                if newElement.Value.(*Mp3AudioFile).removable {
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
                    updatePlaylistFile(playlistPath, mediaSequenceNumber)
                    oOS = false;
                    // TODO: when to set this to true?
                }
            }
        }
        clearMp3FileList(mp3Dir)
        fmt.Printf("HTTP streaming channel closed, stopping.\n")
    }()
    
    // Set up the HTTP page handlers
    mux.HandleFunc("/", func(out http.ResponseWriter, in *http.Request) {
        if !filterCrossDomainRequest(out, in) {
            addCrossDomainToResponse(out)
            if oOS && (oOSDir != ""){
                homeHandler(out, in, oOSDir)
            } else {
                homeHandler(out, in, mp3Dir)
            }
        }
    })
    mux.HandleFunc(mp3Dir + "/", func(out http.ResponseWriter, in *http.Request) {
        if !filterCrossDomainRequest(out, in) {
            addCrossDomainToResponse(out)
            streamHandler(out, in)
        }
    })
    if oOSDir != "" {
        mux.HandleFunc(oOSDir + "/", func(out http.ResponseWriter, in *http.Request) {
            if !filterCrossDomainRequest(out, in) {
                addCrossDomainToResponse(out)
                streamHandler(out, in)
            }
        })
    }
    
    fmt.Printf("Starting HTTP server for Chuff requests on port %s.\n", port)
    
    // Start the HTTP server (should block)
    err = http.ListenAndServeTLS(":" + port, "cert.pem", "privkey.pem", mux)
    
    if err != nil {        
        fmt.Fprintf(os.Stderr, "Could not start HTTP server (%s).\n", err.Error())
    }
}

/* End Of File */
