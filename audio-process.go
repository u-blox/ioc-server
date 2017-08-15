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
    "os"
    "io/ioutil"
    "container/list"
    "bytes"
    "github.com/u-blox/ioc-server/lame"
//    "encoding/hex"
)

//--------------------------------------------------------------------
// Constants
//--------------------------------------------------------------------

// How big the processedDatagramsList can become
const NUM_PROCESSED_DATAGRAMS int = 1

// Guard against silly sequence number gaps
const MAX_GAP_FILL_MILLISECONDS int = 500

// The amount of audio in each MP3 output file
const MAX_MP3_FILE_LENGTH time.Duration = time.Second * 5

//--------------------------------------------------------------------
// Variables
//--------------------------------------------------------------------

// The channel that processes incoming datagrams
var ProcessDatagramsChannel chan<- interface{}

// The list of new datagrams received
var newDatagramList = list.New()

// Place to save already processed datagrams in case we need them again
var processedDatagramList = list.New()

// An audio buffer to hold raw PCM samples received from the client
var pcmAudio bytes.Buffer

// An audio buffer to hold MP3 encoded samples
var mp3Audio bytes.Buffer

// Debug stuff
var bytesDuringInterval int
var lastIntervalTime time.Time
var rate float64
var averagingInterval time.Duration = time.Second * 10

//--------------------------------------------------------------------
// Functions
//--------------------------------------------------------------------

// Open an MP3 file
func openMp3File(dirName string) *os.File {    
    handle, err := ioutil.TempFile (dirName, "mp3_")
    if err == nil {
        log.Printf("Opened \"%s\" for MP3 output.\n", handle.Name())
    } else {
        log.Printf("Unable to create temporary file for MP3 output in directory \"%s\".\n", dirName)
    }
    
    return handle
}

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
        pcmAudio.Write(fill)
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
    pcmAudio.Write(audioBytes)
    
    // If the block is shorter than expected, handle that gap too
    if len(*datagram.Audio) < SAMPLES_PER_BLOCK {
        handleGap(SAMPLES_PER_BLOCK - len(*datagram.Audio), previousDatagram)        
    }
        
    // DEBUG STUFF FROM HERE ON    
    
    log.Printf("Seq %d%s, time %3.3f, length %d sample(s), sum %d, throughput %3.3f kbits/s\n",
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
func encodeOutput (mp3Writer *lame.LameWriter, pcmHandle *os.File) time.Duration {
    var err error
    var x int
    var length time.Duration
    buffer := make([]byte, 1000)
    
    for err == nil {
        x, err = pcmAudio.Read(buffer)
        if x > 0 {
            length = time.Duration(x / URTP_SAMPLE_SIZE * 1000000 / SAMPLING_FREQUENCY) * time.Microsecond
            log.Printf("Encoding %d byte(s) into the output...\n", x)
//            log.Printf("%s\n", hex.Dump(buffer[:x]))
            if mp3Writer != nil {
                _, err = mp3Writer.Write(buffer[:x])
                if err != nil {
                    log.Printf("Unable to encode MP3.\n")
                }
            }
            if pcmHandle != nil {
                _, err = pcmHandle.Write(buffer[:x])
                if err != nil {
                    log.Printf("Unable to write to PCM file.\n")
                }
            }
        }
    }
    
    return length
}

// Do the processing; this function should never return
func operateAudioProcessing(pcmHandle *os.File, mp3Dir string) {
    var mp3Writer *lame.LameWriter
    var mp3Handle *os.File
    var err error
    var mp3Length time.Duration
    var channel = make(chan interface{})
    processTicker := time.NewTicker(time.Duration(BLOCK_DURATION_MS) * time.Millisecond)
    
    ProcessDatagramsChannel = channel
    
    // Initialise the linked list of datagrams
    newDatagramList.Init()

    // Initialise the MP3 encoder.  This is equivalent to:
    // lame -V2 -r -s 16000 -m m --bitwidth 16 <input file> <output file>
    mp3Writer = lame.NewWriter(&mp3Audio)
    mp3Writer.Encoder.SetInSamplerate(SAMPLING_FREQUENCY)
    mp3Writer.Encoder.SetNumChannels(1)
    mp3Writer.Encoder.SetMode(lame.MONO)
    mp3Writer.Encoder.SetVBR(lame.VBR_DEFAULT)
    mp3Writer.Encoder.SetVBRQuality(2)
    // Note: bit depth defaults to 16
    if mp3Writer.Encoder.InitParams() < 0 {
        fmt.Fprintf(os.Stderr, "Unable to initialise Lame for MP3 output.\n")
        os.Exit(-1)
    }
    
    // Create the first mp3 output file
    mp3Handle = openMp3File(mp3Dir)
    if mp3Handle == nil {
        fmt.Fprintf(os.Stderr, "Unable to create temporary file for MP3 output in directory \"%s\" (permissions?).\n", mp3Dir)
        os.Exit(-1)
    }
    
    fmt.Printf("Audio processing channel created and now being serviced.\n")
    
    // Timed function that processes received datagrams and feeds the output stream
    go func() {
        for _ = range processTicker.C {            
            // Go through the list of newly arrived datagrams, processing them and moving
            // them to the processed list
            thingProcessed := false
            for newElement := newDatagramList.Front(); newElement != nil; newElement = newElement.Next() {
                processDatagram(newElement.Value.(*UrtpDatagram), processedDatagramList)
                log.Printf("%d byte(s) in the outgoing audio buffer.\n", pcmAudio.Len())
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
            mp3Length += encodeOutput(mp3Writer, pcmHandle);            
            // If enough time has passed, write the output to file and
            // tell the audio output channel about it
            if mp3Length >= MAX_MP3_FILE_LENGTH {
                if mp3Handle != nil {
                    log.Printf("Writing %d millisecond(s) of MP3 audio to \"%s\" and closing the file.\n", mp3Length / time.Millisecond, mp3Handle.Name())
                    _, err = mp3Audio.WriteTo(mp3Handle)
                    mp3Handle.Close()
                    if err == nil {
                        // Let the audio output channel know of the new audio file
                        mp3AudioFile := new(Mp3AudioFile)
                        mp3AudioFile.filePath = mp3Handle.Name()
                        mp3AudioFile.timestamp = time.Now()
                        mp3AudioFile.duration = mp3Length
                        mp3AudioFile.usable = true;
                        MediaControlChannel <- mp3AudioFile
                    } else {
                        log.Printf("Error writing to \"%s\" (%s).\n", mp3Handle.Name(), err.Error())
                    }
                }
                mp3Length = time.Duration(0);
                mp3Handle = openMp3File(mp3Dir)
            }
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
        fmt.Printf("Audio processing channel closed, stopping.\n")
    }()
}

/* End Of File */
