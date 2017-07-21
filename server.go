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
    "net"
    "os"
    "flag"
    "log"
//    "encoding/hex"
)

// This is the Internet of Chuffs, server side.
// The input stream from the IoC client is 16-bit PCM
// audio sampled at 16 kHz, arriving in 20 ms blocks
// that include a sequence number and timestamp.
// This is written to a buffer and then LAME
// (lame.sourceforge.net) is employed to produce
// an MP3 stream that is sent out over RTP.
//
// A possible LAME command line is:
// lame -V2 -r -s 16000 -m m -x --bitwidth 16 <input file> <output file>

//--------------------------------------------------------------------
// Types
//--------------------------------------------------------------------

// Struct to hold a URTP datagram
type UrtpDatagram struct {
    SequenceNumber  uint16
    Timestamp       uint32
    Audio           *[]int16
}

//--------------------------------------------------------------------
// Variables
//--------------------------------------------------------------------

// The duration of a block of incoming audio in ms
const BLOCK_DURATION_MS int = 20

// The sampling frequency of the incoming audio
const SAMPLING_FREQUENCY int = 16000

// The number of samples per block
const SAMPLES_PER_BLOCK int = SAMPLING_FREQUENCY * BLOCK_DURATION_MS / 1000

// The URTP datagram dimensions
const URTP_HEADER_SIZE int = 8
const URTP_SAMPLE_SIZE int = 2
const URTP_DATAGRAM_SIZE int = URTP_HEADER_SIZE + SAMPLES_PER_BLOCK * URTP_SAMPLE_SIZE

// File to write audio output to
var fileHandle *os.File

// Command-line flags
var port = flag.String ("p", "", "the port number to listen on.");
var file = flag.String ("f", "", "a filename to which the received stream should be written (will be truncated if it already exists).");
var Usage = func() {
    fmt.Fprintf(os.Stderr, "\n%s: run the Internet of Chuffs server.  Usage:\n", os.Args[0])
        flag.PrintDefaults()
    }

//--------------------------------------------------------------------
// Functions
//--------------------------------------------------------------------

// Entry point
func main() {
    var numBytesIn int
    var localUdpAddr *net.UDPAddr
    var server *net.UDPConn
    var err error
    line := make([]byte, URTP_DATAGRAM_SIZE)

    log.SetOutput(os.Stdout)
    
    // Deal with the command-line parameters
    flag.Parse()
    
    if *port != "" {
        // Say what we're doing
        fmt.Printf("Waiting to receiving UDP packets on port %s", *port)
        if (*file != "") {
            fmt.Printf(" and writing them to file %s", *file)        
            fileHandle, err = os.Create(*file);
        }
        fmt.Printf(".\n");
        
        if (err == nil) {
            defer fileHandle.Close()
            // Set up the server
            localUdpAddr, err = net.ResolveUDPAddr("udp", ":" + *port)
            if (err == nil) && (localUdpAddr != nil) {
                // Begin listening
                server, err = net.ListenUDP("udp", localUdpAddr)
                if err == nil {
                    // Read UDP packets
                    for numBytesIn, _, err = server.ReadFromUDP(line); (err == nil) && (numBytesIn > 0); numBytesIn, _, err = server.ReadFromUDP(line) {
                        log.Printf("UDP packet of size %d byte(s) received.\n", numBytesIn)
//                        fmt.Printf("%s\n", hex.Dump(line[:numBytesIn]))
                        if (numBytesIn >= URTP_HEADER_SIZE) {
                            // Populate a URTP datagram with the data
                            urtpDatagram := new(UrtpDatagram)
                            log.Printf("URTP header:\n")
                            urtpDatagram.SequenceNumber = uint16(line[2]) << 8 + uint16(line[3])
                            log.Printf("  sequence number %d\n", urtpDatagram.SequenceNumber)
                            urtpDatagram.Timestamp = (uint32(line[4]) << 24) + (uint32(line[5]) << 16) + (uint32(line[6]) << 8) + uint32(line[7])
                            log.Printf("  timestamp       %d\n", urtpDatagram.Timestamp)
                            if (numBytesIn > URTP_HEADER_SIZE) {
                                audio := make([]int16, (numBytesIn - URTP_HEADER_SIZE) / URTP_SAMPLE_SIZE)
                                // Copy in the bytes
                                x := URTP_HEADER_SIZE
                                for y := range audio {
                                    audio[y] = (int16(line[x]) << 8) + int16(line[x + 1])
                                    x += 2 
                                }
                                urtpDatagram.Audio = &audio
                            }
                            log.Printf("URTP samples %d\n", len(*urtpDatagram.Audio))
                            // Send the data to the processing channel
                            processDatagramsChannel <- urtpDatagram
                        }
                    }
                    if err != nil {
                        log.Printf("Error reading from port %v (%s).\n", localUdpAddr, err.Error())
                    } else {
                        log.Printf("UDP read on port %v returned when it should not.\n", localUdpAddr)    
                    }
                } else {
                    log.Printf("Couldn't start UDP server on port %s (%s).\n", *port, err.Error())
                }            
            } else {
                log.Printf("'%s' is not a valid UDP address (%s).\n", *port, err.Error())
            }
            fileHandle.Close()
        } else {
            fmt.Printf("Unable to open file %s (%s).\n", *file, err.Error())
            flag.PrintDefaults()
            os.Exit(-1)
        }                
    } else {
        fmt.Printf("Must specify a port number.\n")
        flag.PrintDefaults()
        os.Exit(-1)
    }
}
