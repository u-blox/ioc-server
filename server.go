package main

import (
    "fmt"
    "net"
    "os"
    "flag"
    "log"
    "time"
)

// This is the Internet of Chuffs, server side.
// The input stream from the IoC client is 24-bit PCM
// audio sampled at 16 kHz, arriving in 20 ms blocks
// that include a sequence number and timestamp.
// This is written to a buffer and then LAME
// (lame.sourceforge.net) is employed to produce
// an MP3 stream that is sent out over RTP.
//
// A possible LAME command line is:
// lame -V2 -r -s 16000 -m m --bitwidth 24 <input file> <output file>

//--------------------------------------------------------------------
// Types
//--------------------------------------------------------------------

const URTP_HEADER_SIZE int = 8
const URTP_SAMPLE_SIZE int = 3
const URTP_BODY_SIZE int = URTP_SAMPLE_SIZE * 160

//--------------------------------------------------------------------
// Variables
//--------------------------------------------------------------------

// Averaging interval for the throughput calculation
var averagingInterval time.Duration = time.Second * 10

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
    var numPackets int
    var numBytesIn int
    var prevPacketTime time.Time
    var prevIntervalTime time.Time
    var bytesDuringInterval int
    var timeNow time.Time
    var sequenceNumber int
    var prevSequenceNumber int = 0
    var timestamp int
    var sum int
    var alarm string
    var rate float64 = 0
    var localUdpAddr *net.UDPAddr
    var server *net.UDPConn
    var fileHandle *os.File
    var err error
    line := make([]byte, 1024)

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
            // Set up the server
            localUdpAddr, err = net.ResolveUDPAddr("udp", ":" + *port)
            if (err == nil) && (localUdpAddr != nil) {
                server, err = net.ListenUDP("udp", localUdpAddr)
                if err == nil {
                    for numBytesIn, _, err = server.ReadFromUDP(line); (err == nil) && (numBytesIn > 0); numBytesIn, _, err = server.ReadFromUDP(line) {
                        numPackets++
                        sequenceNumber = int(line[2]) << 8 + int(line[3])
                        timestamp = (int(line[4]) << 24) + (int(line[5]) << 16) + (int(line[6]) << 8) + int(line[7])
                        _, err = fileHandle.Write(line[8:numBytesIn])
                        sum = 0;
                        for _, y := range line[8:numBytesIn] {
                            sum += int(y);
                        }
                        alarm = ""
                        if (prevSequenceNumber != 0) && (sequenceNumber != prevSequenceNumber + 1) {
                            alarm = "*"
                        }
                        fmt.Printf("\rProtocol %d, seq %d%s, time %3.3f, length %d byte(s), sum %d, throughput %3.3f kbits/s.                ",
                                   line[0], sequenceNumber, alarm, float64(timestamp) / 1000, numBytesIn, sum, rate)
                        timeNow = time.Now();
                        if (timeNow.Sub(prevPacketTime) < averagingInterval) {
                            if (timeNow.Sub(prevIntervalTime) < averagingInterval) {
                                bytesDuringInterval += numBytesIn;
                            } else {
                                rate = float64(bytesDuringInterval) * 8 / timeNow.Sub(prevIntervalTime).Seconds() / 1000
                                bytesDuringInterval = numBytesIn
                                prevIntervalTime = timeNow
                            }
                        } else {
                            bytesDuringInterval = 0
                        }
                        prevSequenceNumber = sequenceNumber;
                        prevPacketTime = timeNow
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
