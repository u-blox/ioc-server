# Introduction

This repo contains the server side of the Internet of Chuffs, written in Golang.

# Installation
To use this repo on Linux, first make sure that you have Lame installed, with something like:

`sudo yum install lame`

(for Windows the necessary Lame library files are included with this repo)

Grab the code and build it with:

`go get github.com/u-blox/ioc-server`

# Usage

To run the code:

`./ioc-server -p xxxx`

...where xxxx is the port number that the `ioc-server` should receive packets on.

# Credits

This repo includes code imported from:

https://github.com/viert/lame

Copyright, and our sincere thanks, remains with the original authors.