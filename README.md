# Introduction

This repo contains the server side of the Internet of Chuffs, written in Golang.

# Installation (on Linux only I'm afraid)
To use this repo, first make sure that you have Lame installed, with something like:

`sudo yum install lame`

You will then need to create a symlink to the library versions it has installed.  For instance, if the installed Lame library was:

`/usr/lib64/libmp3lame.so.0`

...then you would create the symlink `libmp3lame.so` as follows:

`sudo ln -s /usr/lib64/libmp3lame.so.0 /usr/lib64/libmp3lame.so`

Grab the code and build it with:

`go get github.com/u-blox/ioc-server`

# Usage

To run the code, do something like:

`./ioc-server -p 1234 -o audio.mp3 -r audio.pcm -l ioc-server.log`

...where:

- `1234` is the port number that `ioc-server` should receive packets on,
- `audio.mp3` is the (optional) MP3 output file,
- `audio.pcm` is the (optional) raw 16-bit PCM output file,
- `ioc-server.log` will contain the log output from `ioc-server`.

# Credits

This repo includes code imported from:

https://github.com/viert/lame

Copyright, and our sincere thanks, remains with the original author(s).