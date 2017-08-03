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
    "github.com/gorilla/mux"
)

//--------------------------------------------------------------------
// Types 
//--------------------------------------------------------------------

// Open a stream to a HTTP client
type OpenStream struct {
    id string
}

//--------------------------------------------------------------------
// Variables
//--------------------------------------------------------------------

// The control channel for media streaming out to users
var MediaControlChannel chan<- interface{}

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

// Start HTTP server for streaming output; this function should never return
func operateAudioOut(port string) {
    var channel = make(chan interface{})
    var err error
    streamTicker := time.NewTicker(time.Second)
    
    MediaControlChannel = channel
    
    // Timed function to perform operations on the stream
    go func() {
        for _ = range streamTicker.C {            
        }        
    }()
    
    // Process media control commands
    go func() {
        for cmd := range channel {
            switch message := cmd.(type) {
                // Handle the media control messages
                case *OpenStream:
                {
                    log.Printf("Opening stream %s...\n", message.id)                    
                }
            }
        }
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
