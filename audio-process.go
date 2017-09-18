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
    "path/filepath"
    "io/ioutil"
    "container/list"
    "bytes"
    "encoding/binary"
    "errors"
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
const MAX_MP3_FILE_DURATION time.Duration = time.Second * 15

// The track title to use
const MP3_TITLE string = "Internet of Chuffs"

// The length of the binary timestamp in the ID3 tag of the MP3 file
const MP3_ID3_TAG_TIMESTAMP_LEN int = 8

// The number of samples in an MP3 frame
const MP3_SAMPLES_PER_FRAME int = 576

// The duration of an MP3 frame 
const MP3_FRAME_DURATION time.Duration = time.Duration(uint64(MP3_SAMPLES_PER_FRAME) * 1000000 / uint64(SAMPLING_FREQUENCY)) * time.Microsecond

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

// Prefix that represents the fixed portion of a "PRIV" ID3 tag to put at the start of a
// segment file, see https://tools.ietf.org/html/draft-pantos-http-live-streaming-23#section-3.4
// and http://id3.org/id3v2.3.0#ID3v2_overview
//
// The generic portion of the prefix consists of:
//   - a 10-byte ID3 header, containing:
//     - the characters "ID3",
//     - two bytes of ID3 version number, set to 0x0400,
//     - one byte of ID3 flags, set to 0,
//     - four bytes of ID3 tag size where the most significant bit (bit 7) is set to
//       zero in every byte, making a total of 28 bits; the zeroed bits are ignored, so
//       a 257 bytes long tag is represented as 0x00 0x00 0x02 0x01; in our case
//       the size is 0x3f (63).
//   - an ID3 body, containing:
//     - four characters of frame ID, in our case "PRIV",
//     - four bytes of size, calculated as the whole ID frame size minus the 10-byte ID3 header
//       so in our case 0x35 (53),
//     - two bytes of flags, set to 0.
// The "PRIV" ID3 tag, which is used in our case, consists of:
//   - an owner identifier string followed by 0x00, in our case "com.apple.streaming.transportStreamTimestamp\x00",
//   - MP3_ID3_TAG_TIMESTAMP_LEN octets of big-endian binary timestamp on a 90 kHz basis.
//
// Only the fixed portion of the PRIV ID3 tag is included in this variable, the MP3_ID3_TAG_TIMESTAMP_LEN bytes of timestamp must be
// written separately.
var id3Prefix string = "ID3\x04\x00\x00\x00\x00\x00\x3fPRIV\x00\x00\x00\x35\x00\x00com.apple.streaming.transportStreamTimestamp\x00"

//--------------------------------------------------------------------
// Functions
//--------------------------------------------------------------------

// Open an MP3 file
func openMp3File(dirName string) *os.File {
    handle, err := ioutil.TempFile (dirName, "")
    if err == nil {
        filePath := handle.Name()
        handle.Close()
        if os.Rename(filePath, filePath + SEGMENT_EXTENSION) == nil {
            handle, err = os.Create(filePath + SEGMENT_EXTENSION)
            log.Printf("Opened segment file \"%s\" for MP3 output.\n", handle.Name())
        } else {
            log.Printf("Unable to rename temporary file \"%s\" to \"%s\".\n", filePath, filePath + SEGMENT_EXTENSION)
        }
    } else {
        log.Printf("Unable to create segment file for MP3 output in directory \"%s\".\n", dirName)
    }
    
    return handle
}

// Create an MP3 writer
func createMp3Writer(mp3Audio *bytes.Buffer) *lame.LameWriter {
    // Initialise the MP3 encoder.  This is equivalent to:
    // lame -V2 -r -s 16000 -m m --bitwidth 16 <input file> <output file>
    mp3Writer := lame.NewWriter(mp3Audio)
    if mp3Writer != nil {
        mp3Writer.Encoder.SetInSamplerate(SAMPLING_FREQUENCY)
        mp3Writer.Encoder.SetNumChannels(1)
        mp3Writer.Encoder.SetMode(lame.MONO)
        // VBR writes tags into the file which makes
        // hls.js think the file isn't an MP3 file (as
        // the first MP3 header must appear within the
        // first 100 bytes of the file).  So don't do that.
        mp3Writer.Encoder.SetVBR(lame.VBR_OFF)
        // Disabling the bit reservoir reduces quality
        // but allows consecutive MP3 files to be butted
        // up together without any gaps
        //mp3Writer.Encoder.DisableReservoir()
        mp3Writer.Encoder.SetGenre("144") // Thrash metal
        // Note: bit depth defaults to 16
        if mp3Writer.Encoder.InitParams() >= 0 {
            log.Printf("Created MP3 writer.\n")        
        } else {
            mp3Writer.Close()
            mp3Writer = nil
            log.Printf("Unable to initialise MP3 writer.\n")
        }
    } else {
        log.Printf("Unable to instantiate MP3 writer.\n")
    }
    
    return mp3Writer
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
    
    var previousDatagram *UrtpDatagram
    
    if savedDatagramList.Front() != nil {
        previousDatagram = savedDatagramList.Front().Value.(*UrtpDatagram)
    }
    
    log.Printf("Processing a datagram...\n")
    
    // Handle the case where we have missed some datagrams
    if (previousDatagram != nil) && (datagram.SequenceNumber != previousDatagram.SequenceNumber + 1) {
        handleGap(int(datagram.SequenceNumber - previousDatagram.SequenceNumber) * SAMPLES_PER_BLOCK, previousDatagram)
    }
        
        // Copy the received audio into the buffer    
    if datagram.Audio != nil {
        audioBytes := make([]byte, len(*datagram.Audio) * URTP_SAMPLE_SIZE)
        for x, y := range *datagram.Audio {
            for z := 0; z < URTP_SAMPLE_SIZE; z++ {
                audioBytes[(x * URTP_SAMPLE_SIZE) + z] = byte(y >> ((uint(z) * 8)))
            } 
        }
        log.Printf("Writing %d bytes to the audio buffer...\n", len(audioBytes))
        pcmAudio.Write(audioBytes)
        
        // If the block is shorter than expected, handle that gap too
        if len(*datagram.Audio) < SAMPLES_PER_BLOCK {
            handleGap(SAMPLES_PER_BLOCK - len(*datagram.Audio), previousDatagram)        
        }
    } else {
        // And if the audio is entirely missing, handle that
        handleGap(SAMPLES_PER_BLOCK, previousDatagram)        
    }
}

// Encode the output stream
func encodeOutput (mp3Writer *lame.LameWriter, pcmHandle *os.File) time.Duration {
    var err error
    var x int
    var duration time.Duration
    buffer := make([]byte, 1000)
    
    for err == nil {
        x, err = pcmAudio.Read(buffer)
        if x > 0 {
            duration += time.Duration(x / URTP_SAMPLE_SIZE * 1000000 / SAMPLING_FREQUENCY) * time.Microsecond
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
    
    return duration
}

// Write the ID3 tag to the start of an MP3 segment file indicating
// its time offset from the previous segment file
func writeTag(mp3Handle *os.File, offset time.Duration) error {
    var timestampBytes bytes.Buffer
    var timestampUint64 uint64 // Must be an uint64 to produce the correct sized timestamp
    
    // First, write the prefix
    _, err := mp3Handle.WriteString(id3Prefix)
    if err == nil {
        // Then write the binary timestamp offset on a 90 kHz basis
        timestampUint64 = uint64(float32(offset) / float32(time.Microsecond) * float32(90000) / float32(1000000))
        err := binary.Write(&timestampBytes, binary.BigEndian, timestampUint64)
        if err == nil {
            if timestampBytes.Len() != MP3_ID3_TAG_TIMESTAMP_LEN {
                err = errors.New(fmt.Sprintf("Timestamp is of incorrect size (%d byte(s) (0x%x) when size must be %d byte(s)).\n", timestampBytes.Len(), &timestampBytes, MP3_ID3_TAG_TIMESTAMP_LEN))
            }
        } else {
            log.Printf("Error creating timestamp offset (%s).\n", err.Error())
        }
        
        log.Printf("Writing %d byte timestamp inside MP3 file (0x%x)...\n", timestampBytes.Len(), &timestampBytes)
        _, err = timestampBytes.WriteTo(mp3Handle)
    }
    
    return err
}

// Do the processing; this function should never return
func operateAudioProcessing(pcmHandle *os.File, mp3Dir string) {
    var mp3Audio bytes.Buffer
    var mp3Writer *lame.LameWriter
    var mp3Handle *os.File
    var err error
    var mp3Duration time.Duration
    var mp3Offset time.Duration
    var channel = make(chan interface{})
    processTicker := time.NewTicker(time.Duration(BLOCK_DURATION_MS) * time.Millisecond)
    
    ProcessDatagramsChannel = channel
    
    // Initialise the linked list of datagrams
    newDatagramList.Init()

    // Create the first MP3 writer
    mp3Writer = createMp3Writer(&mp3Audio)
    if mp3Writer == nil {
        fmt.Fprintf(os.Stderr, "Unable to create MP3 writer.\n")
        os.Exit(-1)
    }
    
    // Create the first MP3 output file
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
            mp3Duration += encodeOutput(mp3Writer, pcmHandle);
            
            // If enough time has passed, write the output to file and
            // tell the audio output channel about it
            if mp3Duration >= MAX_MP3_FILE_DURATION {
                if mp3Handle != nil {
                    log.Printf("Writing %d millisecond(s) of MP3 audio to \"%s\".\n", mp3Duration / time.Millisecond, mp3Handle.Name())
                    err = writeTag(mp3Handle, mp3Offset)
                    if err == nil {
                        _, err = mp3Audio.WriteTo(mp3Handle)
                        if mp3Writer != nil {
                            padding, _ := mp3Writer.Close()
                            paddingDuration := time.Duration(uint64(padding) * 1000000 / uint64(SAMPLING_FREQUENCY)) * time.Microsecond
                            log.Printf("Closed MP3 writer, padding was %d, which is %d microseconds.\n", padding, paddingDuration / time.Microsecond)
                            if paddingDuration < mp3Duration {
                                mp3Duration -= paddingDuration
                            }
                        }
                        mp3Handle.Close()
                        log.Printf("Closed MP3 file.\n")
                        if err == nil {
                            // Let the audio output channel know of the new audio file
                            mp3AudioFile := new(Mp3AudioFile)
                            mp3AudioFile.fileName = filepath.Base(mp3Handle.Name())
                            mp3AudioFile.title = MP3_TITLE
                            mp3AudioFile.timestamp = time.Now()
                            mp3AudioFile.duration = mp3Duration
                            mp3AudioFile.usable = true;
                            mp3AudioFile.removable = false;
                            MediaControlChannel <- mp3AudioFile
                        } else {
                            log.Printf("There was an error writing to \"%s\" (%s).\n", mp3Handle.Name(), err.Error())                 
                        }
                    } else {
                        log.Printf("There was an error writing the ID3 tag to \"%s\" (%s).\n", mp3Handle.Name(), err.Error())                 
                    }
                }
                mp3Offset += mp3Duration
                mp3Duration = time.Duration(0)
                mp3Handle = openMp3File(mp3Dir)
                mp3Writer = createMp3Writer(&mp3Audio)
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
                    log.Printf("Adding a new datagram to the FIFO list...\n")
                    newDatagramList.PushBack(datagram)
                }
            }
        }
        fmt.Printf("Audio processing channel closed, stopping.\n")
    }()
}

/* End Of File */
