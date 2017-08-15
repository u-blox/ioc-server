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

package main

import (
    "fmt"
    "log"
    "time"
    "net/http"
    "os"
    "container/list"
    "github.com/gorilla/mux"
)

//--------------------------------------------------------------------
// Types 
//--------------------------------------------------------------------

// Indication that a new MP3 audio file is available
type Mp3AudioFile struct {
    filePath string
    timestamp time.Time
    duration time.Duration
    usable bool
}

// Open a stream to a HTTP client
type OpenStream struct {
    id string
}

//--------------------------------------------------------------------
// Constants
//--------------------------------------------------------------------

// The age at which an MP3 file should no longer be used
const MP3_MAX_AGE time.Duration = time.Minute

//--------------------------------------------------------------------
// Variables
//--------------------------------------------------------------------

// The control channel for media streaming out to users
var MediaControlChannel chan<- interface{}

// List of output mp3 files
var mp3FileList = list.New()

//--------------------------------------------------------------------
// Functions
//--------------------------------------------------------------------

// Provide the handler functions
func handlers() *mux.Router {
    router:= mux.NewRouter()
    router.HandleFunc("/media/chuffs", streamHandler).Methods("GET")    
    return router
}

// Handle a stream request
func streamHandler(out http.ResponseWriter, in *http.Request) {
    out.Header().Set("Content-Type","audio/mpeg")
    
    _, err := mp3Audio.WriteTo(out)
    if err != nil {
        log.Printf("Unable to write to HTTP response.\n")
    }
}

// Empty the MP3 file list, deleting the files as it goes
func clearMp3FileList() {
    log.Printf("Clearing MP3 file list...\n")
    for newElement := newDatagramList.Front(); newElement != nil; newElement = newElement.Next() {
        filePath := newElement.Value.(*Mp3AudioFile).filePath        
        log.Printf("Deleting file \"%s\"...\n", filePath)
        err:= os.Remove(filePath)
        if err != nil {
            log.Printf("Unable to delete \"%s\".\n", filePath)
        }
        newDatagramList.Remove(newElement)
    }
}

// Start HTTP server for streaming output; this function should never return
func operateAudioOut(port string) {
    var channel = make(chan interface{})
    var err error
    streamTicker := time.NewTicker(time.Second * 5)
    
    MediaControlChannel = channel
    
    // Initialise the linked list of MP3 output files
    mp3FileList.Init()

    // Timed function to perform operations on the stream
    go func() {
        for _ = range streamTicker.C {
            // Go through the file list and mark old files as unusable, attempting
            // to delete them as we go 
            for newElement := mp3FileList.Front(); newElement != nil; newElement = newElement.Next() {
                if (newElement.Value.(*Mp3AudioFile).usable) && (time.Now().Sub(newElement.Value.(*Mp3AudioFile).timestamp) > MP3_MAX_AGE) {
                    newElement.Value.(*Mp3AudioFile).usable = false;
                    log.Printf ("MP3 file \"%s\", received at %s, now out of date (time now is %s).\n",
                                newElement.Value.(*Mp3AudioFile).filePath, newElement.Value.(*Mp3AudioFile).timestamp.String(),
                                time.Now().String())
                }
                if !newElement.Value.(*Mp3AudioFile).usable {
                    if os.Remove(newElement.Value.(*Mp3AudioFile).filePath) == nil {
                        log.Printf ("MP3 file \"%s\" successfully deleted and will be removed from the list.\n",
                                    newElement.Value.(*Mp3AudioFile).filePath)
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
                    log.Printf("Adding new MP3 file \"%s\", duration %d millisecond(s), to the list...\n", message.filePath, int(message.duration / time.Millisecond))
                    mp3FileList.PushBack(message)
                }
                case *OpenStream:
                {
                    log.Printf("Opening stream \"%s\"...\n", message.id)                    
                }
            }
        }
        clearMp3FileList()
        fmt.Printf("HTTP streaming channel closed, stopping.\n")
    }()
    
    fmt.Printf("Starting HTTP server for Chuff requests on port %s.\n", port)
    
    http.Handle("/", handlers())
    err = http.ListenAndServeTLS(":" + port, "cert.pem", "privkey.pem", nil)
    
    if err != nil {        
        fmt.Fprintf(os.Stderr, "Could not start HTTP server (%s).\n", err.Error())
    }
}

/* End Of File */
