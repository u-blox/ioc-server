/* Datagram processing function for the Internet of Chuffs server.
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
    "container/list"
    "bytes"
//    "github.com/u-blox/ioc-server/lame"
//    "encoding/hex"
)

//--------------------------------------------------------------------
// Types 
//--------------------------------------------------------------------

// How big the processedDatagramsList can become
const NUM_PROCESSED_DATAGRAMS int = 1

// Guard against silly sequence number gaps
const MAX_GAP_FILL_MILLISECONDS int = 500

//--------------------------------------------------------------------
// Variables
//--------------------------------------------------------------------

// The channel that processes incoming datagrams
var processDatagramsChannel chan<- interface{}

// The list of new datagrams received
var newDatagramList = list.New()

// Place to save datagrams if we need them
var processedDatagramList = list.New()

// An audio buffer to hold raw samples received
// from the client
var rawAudio bytes.Buffer

// Debug stuff
var bytesDuringInterval int
var lastIntervalTime time.Time
var rate float64
var averagingInterval time.Duration = time.Second * 10

//--------------------------------------------------------------------
// Functions
//--------------------------------------------------------------------

// Handle a gap of a given number of samples in the input data
func handleGap(gap int, previousDatagram * UrtpDatagram) {
    fill := make([]byte, gap * URTP_SAMPLE_SIZE)
    var lastValue [URTP_SAMPLE_SIZE]byte
    
    log.Printf("Handling a gap of %d samples...\n", gap)
    if gap < SAMPLING_FREQUENCY * MAX_GAP_FILL_MILLISECONDS / 1000 {
        // TODO: for now just repeat the last sample we received
        if (previousDatagram != nil) && (len(*previousDatagram.Audio) > 0) {        
            for x := 0; x < len(lastValue); x++ {
                lastValue[x] = byte((*previousDatagram.Audio)[len(*previousDatagram.Audio) - 1] >> ((uint(x) * 8)))
            } 
            for x := 0; x < len(fill); x += URTP_SAMPLE_SIZE {
                for y := 0; y < len(lastValue); y++ {
                    fill[x + y] = lastValue[y]
                } 
            } 
        }
        log.Printf("Writing %d bytes to the audio buffer...\n", len(fill))
        rawAudio.Write(fill)
    } else {
        log.Printf("Ignored a silly gap.\n")
    }
}

// Process a URTP datagram
func processDatagram(datagram * UrtpDatagram, savedDatagramList * list.List) {
    var timeNow time.Time
    var sum int
    var alarm string
    audioBytes := make([]byte, len(*datagram.Audio) * URTP_SAMPLE_SIZE)
    var previousDatagram *UrtpDatagram
    
    if savedDatagramList.Front() != nil {
        previousDatagram = savedDatagramList.Front().Value.(*UrtpDatagram)
    }
    
    log.Printf("Processing a datagram...\n")
    
    // Handle the case where we have missed some datagrams
    if (previousDatagram != nil) && (datagram.SequenceNumber != previousDatagram.SequenceNumber + 1) {
        handleGap(int(datagram.SequenceNumber - previousDatagram.SequenceNumber) * SAMPLES_PER_BLOCK, previousDatagram)
        alarm = "*"
    }
    
    // Copy the received audio into the buffer    
    for x, y := range *datagram.Audio {
        for z := 0; z < URTP_SAMPLE_SIZE; z++ {
            audioBytes[(x * URTP_SAMPLE_SIZE) + z] = byte(y >> ((uint(z) * 8)))
        } 
        sum += int(y);
    }
    log.Printf("Writing %d bytes to the audio buffer...\n", len(audioBytes))
    rawAudio.Write(audioBytes)
    
    // If the block is shorter than expected, handle that gap too
    if len(*datagram.Audio) < SAMPLES_PER_BLOCK {
        handleGap(SAMPLES_PER_BLOCK - len(*datagram.Audio), previousDatagram)        
    }
        
    // DEBUG STUFF FROM HERE ON    
    
    log.Printf("Seq %d%s, time %3.3f, length %d byte(s), sum %d, throughput %3.3f kbits/s\n",
               datagram.SequenceNumber, alarm, float64(datagram.Timestamp) / 1000, len(*datagram.Audio), sum, rate)
    timeNow = time.Now();
    bytesDuringInterval += len(*datagram.Audio) * URTP_SAMPLE_SIZE
    if !lastIntervalTime.IsZero() && (timeNow.Sub(lastIntervalTime) > averagingInterval) {
        rate = float64(bytesDuringInterval) * 8 / timeNow.Sub(lastIntervalTime).Seconds() / 1000
        bytesDuringInterval = 0
        lastIntervalTime = timeNow
    }
}

// Encode the output stream
func encodeOutput () {
    var err error
    var x int
    buffer := make([]byte, 1000)
    
    for err == nil {
        x, err = rawAudio.Read(buffer)
        if x > 0 {
            log.Printf("Encoding %d byte(s) into the output...\n", x)
            if fileHandle != nil {
                fileHandle.Write(buffer[:x])
            }
//            fmt.Printf("%s\n", hex.Dump(buffer[:x]))
        }
    }
}

// Do the processing
func operateProcess() {
    var channel = make(chan interface{})
    processTicker := time.NewTicker(time.Duration(BLOCK_DURATION_MS) * time.Millisecond)
    
    processDatagramsChannel = channel
    
    fmt.Printf("Datagram processing channel created and now being serviced.\n")
    
    // Initialise the linked list of datagrams
    newDatagramList.Init()
    
    // Timed function that processes received datagrams and feeds the output stream
    go func() {
        for _ = range processTicker.C {            
            // Go through the list of newly arrived datagrams, processing them and moving
            // them to the processed list
            thingProcessed := false
            for newElement := newDatagramList.Front(); newElement != nil; newElement = newElement.Next() {
                processDatagram(newElement.Value.(*UrtpDatagram), processedDatagramList)
                log.Printf("%d bytes in the outgoing audio buffer.\n", rawAudio.Len())
                log.Printf("Moving datagram from the new list to the processed list...\n")
                processedDatagramList.PushFront(newElement.Value)
                thingProcessed = true
                newDatagramList.Remove(newElement)
            }
            if thingProcessed {
                count := 0
                for processedElement := processedDatagramList.Front(); processedElement != nil; processedElement = processedElement.Next() {
                    count++
                    if count > NUM_PROCESSED_DATAGRAMS {
                        log.Printf("Removing a datagram from the processed list...\n")
                        processedDatagramList.Remove(processedElement)
                        log.Printf("%d datagram(s) now in the processed list.\n", processedDatagramList.Len())
                    }
                }
            }
            // Always need to encode something into the output stream
            encodeOutput();
        }        
    }()
    
    // Process datagrams received on the channel
    go func() {
        for cmd := range channel {
            switch datagram := cmd.(type) {
                // Handle datagrams, throw everything else away
                case *UrtpDatagram:
                {
                    log.Printf("Adding a new datagram to the list...\n")
                    newDatagramList.PushBack(datagram)
                }
            }
        }
        fmt.Printf("Datagram processing channel closed, stopping.\n")
    }()
}

func init() {
    operateProcess()
}

/* End Of File */
